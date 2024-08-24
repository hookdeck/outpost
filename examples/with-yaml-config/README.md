# Example Docker deployment with custom YAML config

## 0. Make sure you have EventKit's Docker image locally

During development when EventKit's image is not live, you may need to manually build the Docker image from source.

```sh
# from hookdeck/EventKit directory
$ docker build -t eventkit .
```

## 1. Build your image

```sh
# from hookdeck/EventKit/examples/with-yaml-config
$ docker build -t myeventkit .
```

## 2. Run EventKit with your custom config

```sh
# from hookdeck/EventKit/examples/with-yaml-config
$ docker run -p 4000:4000 myeventkit # run all 3 services in one process
# or
$ docker run -p 4000:4000 myeventkit --service api
$ docker run myeventkit --service log
$ docker run myeventkit --service delivery
```
