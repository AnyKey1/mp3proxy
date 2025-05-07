up:
	docker run -d -p 8080:8080 --name mp3proxy mp3proxy

build:
	docker build -t mp3proxy .

