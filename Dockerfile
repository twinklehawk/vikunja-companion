FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

LABEL org.opencontainers.image.title=vikunja-companion
LABEL org.opencontainers.image.url=https://github.com/twinklehawk/vikunja-companion
LABEL org.opencontainers.image.source=https://github.com/twinklehawk/vikunja-companion
LABEL org.opencontainers.image.description="Vikunja Companion"
LABEL org.opencontainers.image.licenses=MIT

WORKDIR /app
COPY vikunja-companion ./

CMD ["./vikunja-companion"]
