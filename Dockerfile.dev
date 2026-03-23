# Dockerfile para el backend Go (Desarrollo con Hot Reload)
FROM golang:1.24-alpine

WORKDIR /app

# Configurar el toolchain para permitir versiones superiores si es necesario
ENV GOTOOLCHAIN=auto

# Instalar dependencias adicionales para compilar con CGO desactivado si es necesario
RUN apk add --no-cache git gcc musl-dev

# Instalar Air para Hot Reload
RUN go install github.com/air-verse/air@latest

# Copiar archivos de módulos
COPY go.mod go.sum ./
RUN go mod download

# Copiar el resto del código fuente
COPY . .

EXPOSE 8080

# Usar air para iniciar con hot reload
CMD ["air", "-c", ".air.toml"]
