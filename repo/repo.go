package repo

import (
	"html/template"
	"log"
	"sort"

	"github.com/longkai/xiaolongtongxue.com/helper"
)

// Repo the articles repository.
type Repo interface {
	List(since string, size int) []Doc
	Get(path string) (Doc, error)
	Del(path string)
	Put(path string)
	Post(path string)
	Batch(adds, mods, dels []string)
}

// Visitor post render operation.
type Visitor interface {
	Visit(docs Docs)
}

type entry struct {
	ready chan struct{}
	val   template.HTML
	err   error
}

func (e *entry) call(path string, r Renderer) {
	val, err := helper.Try(3, func() (interface{}, error) { return r.Render(path) })
	e.val, e.err = val.(template.HTML), err
	close(e.ready)
}

type listReq struct {
	path string
	size int
	resp chan Docs
}

type getReq struct {
	path string
	resp chan getResp
}

type getResp struct {
	doc Doc
	err error
}

type reqs struct {
	get  chan getReq
	put  chan string
	del  chan string
	post chan Docs
	list chan listReq
}

// Repository articles repository implements.
type Repository struct {
	reqs

	dir       Dir
	renderer  Renderer
	processor Processor
	visitors  []Visitor

	docs  Docs              // More read, less write.
	index map[string]int    // Fast lookup.
	cache map[string]*entry // Rendering cache.
}

func (r *Repository) loop() {
	for {
		select {
		case req := <-r.reqs.get:
			r.get(req)
		case path := <-r.reqs.put:
			r.put(path)
		case path := <-r.reqs.del:
			r.del(path)
		case doc := <-r.reqs.post:
			r.post(doc)
		case req := <-r.reqs.list:
			r.list(req)
		}
	}
}

func (r *Repository) get(req getReq) {
	i, ok := r.index[req.path]
	if !ok {
		go func() { req.resp <- getResp{Doc{}, NotFoundError(req.path)} }()
		return
	}

	doc := r.docs[i]

	// A hidden doc has no newer/older navigation.
	if !doc.Hide {
		if docs := r.docs.travel(i+1, 1, true, r.docs.filterHidden); docs.Len() > 0 {
			doc.Older = docs[0].URL
		}
		if docs := r.docs.travel(i-1, 1, false, r.docs.filterHidden); docs.Len() > 0 {
			doc.Newer = docs[0].URL
		}
	}

	e := r.cache[req.path]
	if e == nil {
		// Cache misses.
		e = &entry{ready: make(chan struct{})}
		go e.call(doc.Path, r.renderer)
		r.cache[req.path] = e
	}

	go func() {
		<-e.ready
		doc.Body = e.val
		req.resp <- getResp{doc, e.err}
	}()
}

func (r *Repository) put(path string) {
	// Delete then repost.
	log.Printf("revalidate: %s", path)
	r.del(path)
	go r.processor.Process(path)
}

func (r *Repository) del(path string) {
	if i, ok := r.index[path]; ok {
		log.Printf("delete %s", path)
		// Rebuild index, partially.
		for j := i + 1; j < r.docs.Len(); j++ {
			r.index[r.docs[j].URL]--
		}
		delete(r.index, path)
		// Delete from slice.
		copy(r.docs[i:], r.docs[i+1:])
		r.docs = r.docs[:len(r.docs)-1]
		// Clear its rendering cache.
		delete(r.cache, path)
	}
}

func (r *Repository) post(docs Docs) {
	r.docs = append(r.docs, docs...)
	sort.Sort(r.docs)

	// Rebuild index.
	m := make(map[string]int)
	for i, d := range r.docs {
		m[d.URL] = i
	}
	r.index = m

	log.Printf("receive %d new articles, total %d", docs.Len(), r.docs.Len())
}

func (r *Repository) list(req listReq) {
	i, ok := r.index[req.path]
	if !ok {
		i = 0 // If not found any match, start from 0.
	} else {
		i++ // Skip the current one.
	}

	res := r.docs.travel(i, req.size, true, r.docs.filterHidden)

	go func() { req.resp <- res }()
}

// List articles since a specific path, excluded.
func (r *Repository) List(since string, size int) []Doc {
	resp := make(chan Docs)
	r.reqs.list <- listReq{since, size, resp}
	return <-resp
}

// Get a document for the path.
func (r *Repository) Get(path string) (Doc, error) {
	// Read only index, fast indexing without channel synchronization.
	// It's safe since lookup success, the go-routine will lookup again.
	// Hence, when lookup fail, maybe it's just removed or never exists.
	if _, ok := r.index[path]; !ok {
		return Doc{}, NotFoundError(path)
	}
	resp := make(chan getResp)
	r.reqs.get <- getReq{path, resp}
	v := <-resp
	return v.doc, v.err
}

// Del a document for the path.
func (r *Repository) Del(path string) {
	r.reqs.del <- path
}

// Put revalidate a document.
func (r *Repository) Put(path string) {
	r.reqs.put <- path
}

// Post process the path for documents.
func (r *Repository) Post(path string) {
	log.Printf("post %s", path)
	go r.processor.Process(path)
}

// Batch batch reqeust.
func (r *Repository) Batch(adds, mods, dels []string) {
	log.Printf("Batch(%v, %v, %v)", adds, mods, dels)
	// Git only tracks files, hence, all of the slice are files path.
	handle := func(a []string, f func(s string)) {
		for _, v := range a {
			f(v)
		}
	}

	handle(adds, func(f string) {
		go func() {
			docs := r.processor.Process(f)
			for _, v := range r.visitors {
				v.Visit(docs)
			}
		}()
	})
	handle(mods, func(f string) { go r.Put(r.dir.URLPath(f)) })
	handle(dels, func(f string) { go r.Del(r.dir.URLPath(f)) })
}

// NewRepo create a new article repository.
func NewRepo(repoDir string, skipDirs, globDocs []string, user, repo string, vistors ...Visitor) Repo {
	dir := Dir(repoDir)

	p := &DocProcessor{dir: dir}
	p.scanner = &DocScanner{
		skipDirs: skipDirs,
		globDocs: globDocs,
		dir:      dir,
	}
	p.parser = &DocParser{}

	r := new(Repository)
	r.dir = dir
	r.cache = make(map[string]*entry)
	r.index = make(map[string]int)
	r.processor = p
	r.visitors = vistors

	r.reqs = reqs{
		list: make(chan listReq),
		del:  make(chan string),
		put:  make(chan string),
		get:  make(chan getReq),
		post: make(chan Docs),
	}

	r.renderer = NewRenderer(user, repo, dir)

	// Receive result asynchronously.
	p.callback = func(docs Docs) { r.reqs.post <- docs }

	go r.loop()
	r.Post(repoDir)
	return r
}
