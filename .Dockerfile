FROM alpine
COPY ./dist/gitgrope_linux_amd64_v*/gitgrope /usr/bin/gitgrope
ENTRYPOINT [ "/usr/bin/gitgrope", ".grope.yaml" ]