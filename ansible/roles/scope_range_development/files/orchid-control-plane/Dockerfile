FROM python:3.12-alpine

WORKDIR /app
COPY app.py /app/app.py

EXPOSE 8080
USER 65532:65532
CMD ["python3", "/app/app.py"]
