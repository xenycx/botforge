# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/build/ /src/internal/webui/dist/
ARG VERSION=container
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/botpanel ./cmd/botpanel

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/botpanel /usr/local/bin/botpanel
COPY runtimes /opt/botforge/runtimes
WORKDIR /var/lib/botpanel
ENV BOTPANEL_ENV=production \
    BOTPANEL_LISTEN=0.0.0.0:8080 \
    BOTPANEL_DB_PATH=/var/lib/botpanel/botpanel.db \
    BOTPANEL_DATA_ROOT=/var/lib/botpanel/workspaces \
    BOTPANEL_KEY_DIR=/var/lib/botpanel/keys \
    BOTPANEL_RUNTIMES_DIR=/opt/botforge/runtimes
VOLUME ["/var/lib/botpanel"]
EXPOSE 8080 2022
ENTRYPOINT ["botpanel"]
