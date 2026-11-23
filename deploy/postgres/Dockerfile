# Shigoto's PostgreSQL: the pinned upstream Alpine image plus pgvector.
# Stays on Alpine (musl) so an existing data directory keeps the same
# collation behaviour; switching libc can silently corrupt text indexes.
ARG POSTGRES_IMAGE=postgres:18.6-alpine@sha256:123b17998ccaf47e53b502e8c6a1e79afd8f579c6f3eec83c563da026f957395

FROM ${POSTGRES_IMAGE} AS build
ARG PGVECTOR_VERSION=0.8.7
RUN apk add --no-cache --virtual .build git build-base
RUN git clone --depth 1 --branch v${PGVECTOR_VERSION} https://github.com/pgvector/pgvector.git /src/pgvector
WORKDIR /src/pgvector
# OPTFLAGS="" avoids -march=native: the image must run on any CPU of the
# target architecture, not only the build host's. with_llvm=no skips JIT
# bitcode, which would need the exact clang version Postgres was built with.
RUN make OPTFLAGS="" with_llvm=no \
 && make install with_llvm=no DESTDIR=/out

FROM ${POSTGRES_IMAGE}
COPY --from=build /out/ /
