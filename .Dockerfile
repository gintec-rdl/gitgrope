FROM alpine
COPY ./dist/gitgrope* /usr/bin/gitgrope
ENTRYPOINT [ "/usr/bin/gitgrope", ".grope.yaml" ]