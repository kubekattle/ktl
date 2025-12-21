FROM golang:1.22-bookworm

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      git \
      ruby \
      ruby-dev \
      build-essential \
      rpm \
      dpkg-dev && \
    gem install --no-document fpm && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY packaging/build-packages.sh /usr/local/bin/build-packages
RUN chmod +x /usr/local/bin/build-packages

ENTRYPOINT ["/usr/local/bin/build-packages"]
