FROM golang:1.21.6-alpine

WORKDIR /app

COPY . .

RUN go mod download
RUN go build -o main main.go

EXPOSE 8084

CMD ["./main", "start"]