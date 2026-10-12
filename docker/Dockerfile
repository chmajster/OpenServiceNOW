# syntax=docker/dockerfile:1
FROM golang:1.27-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN set -eu; for command in api migrate bootstrap worker scheduler; do \
      CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/opensnow-$command ./cmd/$command; \
    done

FROM node:22-bookworm-slim AS web-build
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM debian:bookworm-slim AS backend
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 opensnow \
    && useradd --uid 1000 --gid 1000 --no-create-home --shell /usr/sbin/nologin opensnow
RUN mkdir -p /var/lib/opensnow/attachments && chown 1000:1000 /var/lib/opensnow/attachments
WORKDIR /app
COPY --from=go-build /out/ /usr/local/bin/
COPY --chmod=0755 docker/entrypoint.sh /usr/local/bin/opensnow-entrypoint
COPY migrations ./migrations
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["opensnow-entrypoint"]
CMD ["api"]

FROM nginxinc/nginx-unprivileged:1.28-alpine AS frontend
COPY --from=web-build /src/dist /usr/share/nginx/html
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
WORKDIR /srv/opensnow
EXPOSE 8080
