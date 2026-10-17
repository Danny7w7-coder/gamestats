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

# Usuario sin privilegios; es dueño de /app para poder escribir logs/.
RUN adduser -D -H app && chown app /app
USER app

COPY --from=build /out/ingestion /out/simulator /app/

# Puerto por defecto (variable PORT). En Docker, SCALA_URL se pasa desde
# docker-compose (http://analytics:8081).
EXPOSE 8080
CMD ["/app/ingestion"]
