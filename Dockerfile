# builder
FROM golang:1.26-trixie AS build
WORKDIR /src
COPY . /src
RUN CGO_ENABLED=0 go build -o app .

# runner
FROM debian:trixie-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        tzdata \
        curl \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

ARG GITHUB_TAG
ENV GITHUB_TAG=${GITHUB_TAG}
ENV TZ="UTC"

WORKDIR /app
COPY --from=build /src/app /app
COPY . /app


RUN echo "$GITHUB_TAG" > /app/version
RUN echo /app/version

EXPOSE 8080

CMD ["./app"]
