# --- Etapa 1: compilar el fat jar con sbt -------------------------------------
FROM eclipse-temurin:21-jdk AS build
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && curl -fsSL https://github.com/sbt/sbt/releases/download/v1.10.7/sbt-1.10.7.tgz | tar xz -C /opt \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/opt/sbt/bin:${PATH}"
WORKDIR /app

# Primero solo la definición del build: esta capa (dependencias descargadas)
# se reutiliza mientras build.sbt y project/ no cambien.
COPY build.sbt ./
COPY project ./project
RUN sbt -batch update

COPY src ./src
RUN sbt -batch assembly

# --- Etapa 2: solo el JRE y el jar ----------------------------------------------
# eclipse-temurin trae curl, que usa el healthcheck del docker-compose.
FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=build /app/target/scala-3.3.4/analytics.jar ./analytics.jar
ENV PORT=8081 RESULTS_DIR=/data
VOLUME /data
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=3 \
    CMD curl -fs http://localhost:8081/health || exit 1
CMD ["java", "-XX:MaxRAMPercentage=75", "-jar", "analytics.jar"]
