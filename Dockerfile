FROM centos:latest

MAINTAINER longkai <im.longkai@gmail.com>

# Install Git
RUN yum -y install git

# Install Golang
ARG go=1.7
ADD https://storage.googleapis.com/golang/go${go}.linux-amd64.tar.gz go.tar.gz
RUN tar -C /usr/local -xzf go.tar.gz && rm -rf go.tar.gz
#ADD build/go1.7.linux-amd64.tar.gz /usr/local

ENV GOPATH /go
ENV PATH $PATH:/usr/local/go/bin
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"

# Install Nodejs&Bower
ADD https://nodejs.org/dist/v6.5.0/node-v6.5.0-linux-x64.tar.xz node.tar.xz
RUN tar -C /usr/local -xf node.tar.xz && mv /usr/local/node* /usr/local/node && rm -rf node.tar.xz
#ADD build/node-v6.5.0-linux-x64.tar.xz /usr/local
#RUN mv -f /usr/local/node* /usr/local/node

ENV PATH $PATH:/usr/local/node/bin

RUN npm install -g bower && echo '{ "allow_root": true }' > /root/.bowerrc
#RUN npm --registry https://registry.npm.taobao.org install -g bower && echo '{ "allow_root": true }' > /root/.bowerrc

# This is only a placeholder for check everything is fine. Plz replace yours using build arg at build time or `docker volume` to replace repo dir at runtime
ARG repo=https://github.com/longkai/longkai.git
RUN git clone --depth=1 ${repo} /repo

RUN go get github.com/longkai/xiaolongtongxue.com
WORKDIR $GOPATH/src/github.com/longkai/xiaolongtongxue.com
# Replace our own which has build ID maybe helpful for CDN cache problem
RUN ./build.sh && mv -f ./xiaolongtongxue.com $GOPATH/bin

WORKDIR "$GOPATH/src/github.com/longkai/xiaolongtongxue.com/assets"
RUN bower install

# Replace my fav mdl theme here, you can do it using `docker volume`
ADD https://code.getmdl.io/1.2.0/material.light_green-green.min.css bower_components/material-design-lite/material.min.css

VOLUME ["/repo", "/env"]

EXPOSE 1217

ENTRYPOINT ["$GOPATH/bin/xiaolongtongxue.com", "/env/env.yaml"]
