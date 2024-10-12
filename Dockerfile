FROM golang:1.23.1 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o webhook .

FROM golang:1.23.1-alpine  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/webhook .
EXPOSE 8443
CMD ["./webhook"]
