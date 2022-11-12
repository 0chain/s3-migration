FROM golang:1.18-alpine3.16 as s3mgrt-build

LABEL zchain="s3mgrt"

# https://mirrors.alpinelinux.org/
# RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

RUN echo "https://mirrors.aliyun.com/alpine/v3.14/main" >> /etc/apk/repositories
RUN echo "https://mirrors.aliyun.com/alpine/v3.14/community" >> /etc/apk/repositories

RUN echo "https://sjc.edge.kernel.org/alpine/v3.14/main" >> /etc/apk/repositories
RUN echo "https://sjc.edge.kernel.org/alpine/v3.14/community" >> /etc/apk/repositories

RUN echo "https://uk.alpinelinux.org/alpine/v3.14/main" >> /etc/apk/repositories
RUN echo "https://uk.alpinelinux.org/alpine/v3.14/community" >> /etc/apk/repositories

RUN echo "https://dl-4.alpinelinux.org/alpine/v3.14/main" >> /etc/apk/repositories
RUN echo "https://dl-4.alpinelinux.org/alpine/v3.14/community" >> /etc/apk/repositories



RUN apk add --update --no-cache build-base linux-headers git cmake bash perl grep gmp gmp-dev openssl-dev 

# # Install Herumi's cryptography
# WORKDIR /tmp

# COPY ./docker.local/bin/mcl.tar.gz ./
# COPY ./docker.local/bin/bls.tar.gz ./

# RUN tar zxvf mcl.tar.gz && rm mcl.tar.gz && mv mcl* mcl

# RUN tar zxvf bls.tar.gz && rm bls.tar.gz && mv bls* bls  

# RUN make -C mcl -j $(nproc) lib/libmclbn256.so install 
# RUN cp mcl/lib/libmclbn256.so /usr/local/lib 

# RUN make MCL_DIR=$(pwd)/mcl -C bls -j $(nproc) install 

# RUN rm -R /tmp/mcl && rm -R /tmp/bls

ENV SRC_DIR=/0chain
WORKDIR $SRC_DIR

# Download the dependencies:
# Will be cached if we don't change mod/sum files
COPY ./go.mod ./go.sum $SRC_DIR
RUN go mod download

COPY .  $SRC_DIR
RUN make build

FROM alpine:3.16
RUN apk add gmp gmp-dev openssl-dev
COPY --from=s3mgrt-build  /usr/local/lib/libmcl*.so \
                        /usr/local/lib/libbls*.so \
                        /usr/local/lib/

COPY --from=s3mgrt-build /0chain/s3mgrt /usr/local/bin/s3mgrt