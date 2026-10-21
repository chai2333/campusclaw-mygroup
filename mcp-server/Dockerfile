FROM python:3.12-slim
WORKDIR /app
COPY server.py .
USER 10001:10001
EXPOSE 8090
CMD ["python", "server.py"]
