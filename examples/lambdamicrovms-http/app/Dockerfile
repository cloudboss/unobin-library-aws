FROM docker.io/library/python:3.14.6-trixie

WORKDIR /app
COPY server.py /app/server.py

ENV PORT=8080
ENV PYTHONUNBUFFERED=1

EXPOSE 8080
CMD ["python", "/app/server.py"]
