# Control-plane image: build the SPA, embed it into a static Go binary, ship
# the binary alone. One artifact, no runtime beyond the binary (ADR-0008).

FROM node:22-alpine AS ui
WORKDIR /src/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY ui/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/innerwall ./cmd/innerwall
RUN mkdir -p /out/state

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/innerwall /innerwall
# State directory (signing authority) owned by the runtime user; a volume
# mounted here inherits the ownership on first use.
COPY --from=build --chown=nonroot:nonroot /out/state /var/lib/innerwall
EXPOSE 8080 8443
ENTRYPOINT ["/innerwall"]
