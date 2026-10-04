FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /latexgrambot ./cmd/latexgrambot

# Runtime: TeX Live (pdflatex) and poppler (pdftoppm). The latex-extra,
# pictures, science and plain-generic collections cover the broad default
# preamble (tikz, pgfplots, siunitx, mhchem, chemfig, ...).
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates tzdata \
        texlive-latex-base texlive-latex-recommended texlive-fonts-recommended \
        texlive-latex-extra texlive-pictures texlive-science texlive-plain-generic \
        lmodern poppler-utils \
    && rm -rf /var/lib/apt/lists/* \
    && adduser --disabled-password --gecos "" --uid 1000 appuser
COPY --from=build /latexgrambot /usr/local/bin/latexgrambot

# Preload the default document class and package set into a TeX format, so
# every render skips loading ~50 packages (about a second per compile).
RUN mkdir -p /tmp/format \
    && /usr/local/bin/latexgrambot -print-preamble > /tmp/format/preamble.tex \
    && cd /tmp/format \
    && pdflatex -ini -jobname=latexgrambot "&pdflatex" mylatexformat.ltx preamble.tex >/dev/null \
    && install -d /usr/local/share/texmf/web2c \
    && install -m 0644 /tmp/format/latexgrambot.fmt /usr/local/share/texmf/web2c/latexgrambot.fmt \
    && mktexlsr \
    && rm -rf /tmp/format

USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/latexgrambot"]
