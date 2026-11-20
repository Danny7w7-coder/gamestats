# Imagen del módulo de ingestión de GameStats (Go).
# Contiene dos programas:
#   /app/ingestion  servidor HTTP (comando por defecto)
#   /app/simulator  simulador de partidas (se usa cambiando el comando)

# --- Etapa 1: compilar ---------------------------------------------------
# La versión de Go coincide con la directiva "go 1.24" de go.mod.
# Se compila en la arquitectura de la máquina que construye ($BUILDPLATFORM)
# y Go genera los binarios para la arquitectura de destino (TARGETOS/TARGETARCH),
# así la imagen sale para amd64 y arm64 sin compilar emulado.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src

# Solo librería estándar: no hay dependencias que descargar (no hace falta
# "go mod download"), así que se copia todo el código de una vez.
COPY . .

# CGO_ENABLED=0 genera binarios estáticos que corren en cualquier Linux,
# sin necesitar las librerías de C de la imagen de compilación.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/ingestion ./cmd/ingestion \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/simulator ./cmd/simulator

# --- Etapa 2: imagen final, pequeña ------------------------------------
# No lleva el compilador de Go ni el código fuente: solo los binarios.
FROM alpine:3.20
WORKDIR /app

# Usuario sin privilegios; es dueño de /app para poder escribir logs/ y
# data/. /app/data existe en la imagen (y es de app) para que un volumen
# montado ahí herede ese dueño: si no, Docker lo crea como root y el
# servidor no podría guardar las partidas en curso (DATA_DIR=/app/data).
RUN adduser -D -H app && mkdir /app/data && chown app /app /app/data
USER app

COPY --from=build /out/ingestion /out/simulator /app/

# Puerto por defecto (variable PORT). En Docker, SCALA_URL se pasa desde
# docker-compose (http://analytics:8081). DATA_DIR no tiene valor por defecto:
# para que las partidas en curso sobrevivan a un reinicio, docker-compose pasa
# DATA_DIR=/app/data y monta un volumen en /app/data.
EXPOSE 8080
CMD ["/app/ingestion"]
