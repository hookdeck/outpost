# Example Docker deployment with custom YAML config

## 0. Make sure you have Outpost's Docker image locally

During development when Outpost's image is not live, you may need to manually build the Docker image from source.

```sh
# from hookdeck/outpost directory
$ docker build -t outpost .
```

## 1. Build your image

```sh
# from hookdeck/outpost/examples/with-yaml-config
$ docker build -t myoutpost .
```

## 2. Run Outpost with your custom config

```sh
# from hookdeck/outpost/examples/with-yaml-config
$ docker run -p 4000:4000 myoutpost # run all 3 services in one process
# or
$ docker run -p 4000:4000 myoutpost --service api
$ docker run myoutpost --service log
$ docker run myoutpost --service delivery
```
