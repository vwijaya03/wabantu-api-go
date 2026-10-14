# Image Cloud Run tidak dibuat dari Dockerfile ini.
# Binary Encore yang benar hanya keluar dari `encore build docker`.
# Pakai ./scripts/cloudrun/build-image.sh
FROM alpine:3.21
RUN echo "Jangan docker build file ini. Pakai ./scripts/cloudrun/build-image.sh" && exit 1
