# Build stage
FROM golang:1.21-alpine AS build

WORKDIR /app
# Instala git para go get si hace falta
RUN apk add --no-cache git

# Copia go.mod/go.sum si usas módulos. Si no, Go descargará dependencias.
COPY /app .

# Compila estáticamente (opcional)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /aprs-server aprs-server.go

# Runtime stage
FROM alpine:3.18

# usuario no root
RUN addgroup -S aprs && adduser -S -G aprs aprs

# Crear directorio y copiar binario
WORKDIR /app

COPY --from=build /aprs-server /app/aprs-server
COPY --from=build /app/templates ./templates

# Opcional: carpeta para logs
RUN mkdir -p /var/log/aprs && chown aprs:aprs /var/log/aprs

USER aprs
EXPOSE 8080

CMD ["/app/aprs-server"]
