# Build from source. The release image is Dockerfile.release, which takes a
# binary goreleaser has already built.
FROM golang:1.27rc2-alpine AS build

WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/kvant ./cmd/kvant

FROM alpine:3.22
# poppler-utils gives pdfinfo, pdftotext, pdftoppm and pdffonts, which the
# native extraction path shells out to.
RUN apk add --no-cache poppler-utils ca-certificates
COPY --from=build /out/kvant /usr/local/bin/kvant
ENTRYPOINT ["kvant"]
