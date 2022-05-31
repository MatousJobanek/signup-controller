FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

LABEL maintainer "Devtools <devtools@redhat.com>"
LABEL author "Devtools <devtools@redhat.com>"

ENV OPERATOR=/usr/local/bin/signup-controller \
    USER_UID=1001 \
    USER_NAME=host-operator \
    LANG=en_US.utf8

# install operator binary
COPY bin/signup-controller ${OPERATOR}

COPY bin/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
