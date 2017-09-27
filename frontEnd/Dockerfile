FROM golang:latest

WORKDIR /app

ADD main.go /app/
ADD ./tags.json /app/
COPY ./templates/ /app/templates
RUN  go get gopkg.in/mgo.v2; go get -u github.com/gorilla/mux; go build -o main


ENTRYPOINT ["./main"]