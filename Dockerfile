FROM ubuntu:26.04

RUN apt-get update \
    && apt-get install -y python3-pip

RUN pip install --upgrade pip
RUN pip install 'ansible-core>=2.20.0,<2.21.0'
RUN pip install pywinrm

RUN apt-get update -y && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    sshpass lftp rsync openssh-client

COPY ./ansible/requirements.yml .

RUN ansible-galaxy collection install -r requirements.yml
