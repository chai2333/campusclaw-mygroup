FROM python:3.12-slim
WORKDIR /work
COPY apitest/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt
COPY apitest /work/apitest
RUN mkdir -p /work/apitest/reports
ENTRYPOINT ["python", "apitest/run_all.py"]
