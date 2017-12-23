FROM golang:latest

WORKDIR /app

ADD main.go /app/
COPY ./keys/ /app/keys/

RUN go get github.com/dgrijalva/jwt-go; go get github.com/gorilla/mux; go get gopkg.in/mgo.v2;go get github.com/rs/cors; go build -o main

ENTRYPOINT ["./main"]