FROM python:3.11-slim

# Install system dependencies for OpenCV
RUN apt-get update && apt-get install -y \
    libgl1 \
    libglib2.0-0 \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY main.py .

# Install Python dependencies
RUN pip install opencv-python-headless numpy

# Create volume for recordings
RUN mkdir /recordings
VOLUME /recordings

CMD ["python", "main.py"]
