# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM marcbachmann/libvips:latest

# Server port to listen
ENV PORT 8080

# Go version to use
ENV GOLANG_VERSION 1.8.1

# gcc for cgo
RUN apt-get update && apt-get install -y \
    gcc curl git libc6-dev make ca-certificates \
    --no-install-recommends \
  && rm -rf /var/lib/apt/lists/*

ENV GOLANG_DOWNLOAD_URL https://golang.org/dl/go$GOLANG_VERSION.linux-amd64.tar.gz
ENV GOLANG_DOWNLOAD_SHA256 a579ab19d5237e263254f1eac5352efcf1d70b9dacadb6d6bb12b0911ede8994

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

# Download the godep tool.
RUN go get -u github.com/tools/godep

# Build the gothumb command inside the container.
WORKDIR $GOPATH/src/github.com/opendoor-labs/gothumb
RUN godep go install

# Run the outyet command by default when the container starts.
ENTRYPOINT ["/go/bin/gothumb"]

# Expose the server TCP port
EXPOSE $PORT
