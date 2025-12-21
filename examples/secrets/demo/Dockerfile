FROM alpine:3.20

# This is intentionally "bad": a token is baked into the image filesystem.
RUN mkdir -p /app && echo "ghp_0123456789abcdef0123456789abcdef0123" > /app/token.txt

