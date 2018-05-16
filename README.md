# GoThumb

[![Build Status](https://travis-ci.org/opendoor-labs/gothumb.svg?branch=master)](https://travis-ci.org/opendoor-labs/gothumb)

A very fast [golang](http://golang.org/) port of [thumbor](https://github.com/thumbor/thumbor).

## Features

- [x] Image Resizing via URL
- [x] HMAC url signing
- [x] Caching of resized images in S3
- [x] Parallel S3 cache downloads
- [x] Parallel S3 cache uploads
- [ ] Smart crop support
- [ ] Parallel source file fetching
- [ ] Other storage engines
- [ ] Tests
- [x] Unsafe mode

## HOW-TO
### DEPLOY (Opendoor specific)
Since this is an open source repo, deploy setup is stored in the private [opendoor-labs/opendoor-gothumb](http://github.com/opendoor-labs/opendoor-gothumb) repo. Once master has finished building you can deploy staging with `kubectl apply -f k8s/gothumb-staging.yaml`. Update (and push) the production config (`k8s/gothumb.yaml`) and then deploy it with `kubectl apply -f k8s/gothumb.yaml`

### TEST
#### LOCAL:
Run
```
$ cd .
$ go run main.go -unsafe=true
```
Call `localhost:8888/unsafe/0x500/https://listing-photos-production.s3.amazonaws.com/uploads/work_order_item-237397/1675177-g8UBAKwT8dw.jpg` from your browser.
