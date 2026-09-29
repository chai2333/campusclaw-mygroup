FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN addgroup --system app && adduser --system --ingroup app app

WORKDIR /app
COPY pyproject.toml requirements.txt ./
COPY campusclaw ./campusclaw
COPY container_entrypoint.py wsgi.py ./
RUN pip install --no-cache-dir .

RUN mkdir -p /data/uploads && chown -R app:app /data /app
USER app

EXPOSE 8000
CMD ["python", "container_entrypoint.py"]
