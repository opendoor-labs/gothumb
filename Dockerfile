FROM ubuntu:14.04

# vips version
ENV LIBVIPS_VERSION_MAJOR 8
ENV LIBVIPS_VERSION_MINOR 5
ENV LIBVIPS_VERSION_PATCH 8
ENV LIBVIPS_VERSION $LIBVIPS_VERSION_MAJOR.$LIBVIPS_VERSION_MINOR.$LIBVIPS_VERSION_PATCH

# Install dependencies and vips
RUN apt-get update && \
  DEBIAN_FRONTEND=noninteractive apt-get install -y \
  automake build-essential curl \
  gobject-introspection gtk-doc-tools libglib2.0-dev libjpeg-turbo8-dev libpng12-dev \
  libwebp-dev libtiff5-dev libgif-dev libexif-dev libxml2-dev libpoppler-glib-dev \
  swig libmagickwand-dev libpango1.0-dev libmatio-dev libopenslide-dev libcfitsio3-dev \
  libgsf-1-dev fftw3-dev liborc-0.4-dev librsvg2-dev && \
  cd /tmp && \
  curl -L https://github.com/jcupitt/libvips/releases/download/v$LIBVIPS_VERSION/vips-$LIBVIPS_VERSION.tar.gz | tar xz && \
  cd /tmp/vips-$LIBVIPS_VERSION && \
  ./configure --enable-debug=no --without-python $1 && \
  make && \
  make install && \
  ldconfig && \
  apt-get remove -y curl automake build-essential && \
  apt-get autoremove -y && \
  apt-get autoclean && \
  apt-get clean && \
  rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Server port to listen
ENV PORT 8080

# Go version to use
ENV GOLANG_VERSION 1.9

# gcc for cgo
RUN apt-get update && apt-get install -y \
    gcc curl git libc6-dev make ca-certificates \
    --no-install-recommends \
  && rm -rf /var/lib/apt/lists/*

ENV GOLANG_DOWNLOAD_URL https://golang.org/dl/go$GOLANG_VERSION.linux-amd64.tar.gz
ENV GOLANG_DOWNLOAD_SHA256 d70eadefce8e160638a9a6db97f7192d8463069ab33138893ad3bf31b0650a79

RUN curl -fsSL --insecure "$GOLANG_DOWNLOAD_URL" -o golang.tar.gz \
  && echo "$GOLANG_DOWNLOAD_SHA256 golang.tar.gz" | sha256sum -c - \
  && tar -C /usr/local -xzf golang.tar.gz \
  && rm golang.tar.gz

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"
WORKDIR $GOPATH

# Copy the local package to the container's workspace.
ADD . $GOPATH/src/github.com/opendoor-labs/gothumb

RUN go get -u github.com/golang/dep/cmd/dep

# Build the gothumb command inside the container.
WORKDIR $GOPATH/src/github.com/opendoor-labs/gothumb
RUN go install

# Run the outyet command by default when the container starts.
ENTRYPOINT ["/go/bin/gothumb"]

# Expose the server TCP port
EXPOSE $PORT
