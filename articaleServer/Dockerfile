FROM golang:latest

WORKDIR /app

ADD main.go /app/
ADD sources.json /app/
RUN go get github.com/PuerkitoBio/goquery; go get gopkg.in/mgo.v2; go get github.com/streadway/amqp; go get github.com/nazandr/go-readability; go get github.com/mmcdole/gofeed; go build -o main

ENTRYPOINT ["./main"]
