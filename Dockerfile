FROM cgr.dev/chainguard/go as builder

WORKDIR /build
ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=auto
COPY ./cmd/whodis/* ./cmd/whodis/
COPY ./Makefile .
COPY ./go.mod .
COPY ./go.sum .
RUN make build

FROM cgr.dev/chainguard/static
WORKDIR /app
COPY --from=builder /build/bin/whodis .
CMD ["/app/whodis"]
