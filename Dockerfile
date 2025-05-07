# Используем базу с полноценной glibc
FROM golang:1.22.2-bullseye

# Устанавливаем необходимые зависимости для CGO и SQLite
RUN apt-get update && apt-get install -y \
    gcc \
    sqlite3 \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Включаем CGO
ENV CGO_ENABLED=1

# Копируем зависимости и скачиваем модули
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники
COPY . .

# Собираем бинарник
RUN go build -o mp3proxy .

EXPOSE 8080

CMD ["./mp3proxy"]