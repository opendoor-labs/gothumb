# GoThumb

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
### DEPLOY
Gothumb is on k8bs. Refer to [opendoor-gothumb](https://github.com/opendoor-labs/opendoor-gothumb)

### TEST
#### LOCAL:
Run
```
$ cd .
$ go run main.go -unsafe=true
```
Call `localhost:8888/unsafe/0x500/https://listing-photos-production.s3.amazonaws.com/uploads/work_order_item-237397/1675177-g8UBAKwT8dw.jpg` from your browser.

#### STAGING:

Build the docker image by
```
docker build . -t opendoor/gothumb:staging
```
Log in to docker hub with the opendoor account, if you haven't done so by
`docker login`. Creds are in 1Password.

Push to docker hub
`docker push opendoor/gothumb:staging`
Deploy to staging by first getting the pod
```
kubectl get pods -n staging --selector="app=gothumb"
```
```
NAME                       READY     STATUS    RESTARTS   AGE
gothumb-3265306518-htm14   1/1       Running   0          4h
```
```
kubectl delete pod gothumb-3265306518-htm14 -n staging
```
You can check the status of the pod with
```
kubectl get pods -n staging --selector="app=gothumb"
```
and check the status is `Running`

You need to pass in the signature of your request to test it in staging, unless you are using unsafe mode locally.
You can use the `generature_signature.py` to generate the test uri by passing in the uri of the picture in the form of `/#{width}x#{height}/#{url_of_picture}`
and SECURITY_KEY.

You can find the security key in [opendoor-gothumb](https://github.com/opendoor-labs/opendoor-gothumb) repo.
In the repo,
```
ansible-vault view k8s/secrets.yaml
```
The password can be found in 1Password.
```echo #{SECURITY_KEY} | base64 -D```
to get the decoded security key.

Call the staging gothumb instance, e.g., by `k8s-gothumb-staging.services.opendoor.com/KU5YSf1u75WuyzSGjud8yJerbiY=/0x500/https://listing-photos-production.s3.amazonaws.com/uploads/work_order_item-237397/1675177-g8UBAKwT8dw.jpg`
