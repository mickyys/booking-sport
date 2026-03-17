# Dockerfile para el backend Go (Desarrollo con Hot Reload)
FROM golang:1.25-alpine

WORKDIR /app

# Instalar dependencias necesarias y Air para Hot Reload
RUN apk add --no-cache git && \
    go install github.com/air-verse/air@latest

# Copiar archivos de módulos
COPY go.mod go.sum ./
RUN go mod download

# Copiar el resto del código fuente
COPY . .

EXPOSE 8080

# Usar air para iniciar con hot reload
CMD ["air", "-c", ".air.toml"]
