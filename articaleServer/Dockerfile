FROM golang:latest

WORKDIR /app

ADD main.go /app/
ADD sources.json /app/
RUN go get gopkg.in/mgo.v2; go get github.com/streadway/amqp; go get github.com/mmcdole/gofeed
RUN go build -o main

ENTRYPOINT ["./main"]
