all: snapshot

ssl/server.key ssl/server.crt:
	mkdir -p ssl
	openssl req -subj "/CN=*.localhost" -x509 -newkey rsa:2048 -keyout ssl/server.key -out ssl/server.crt -days 365 -nodes

ssl: ssl/server.key ssl/server.crt

build/git-pipe.1.gz:
	mkdir -p build
	pandoc README.md -s -t man -o build/git-pipe.1
	gzip -f build/git-pipe.1

build/git-pipe:
	mkdir -p build
	go build -v -o build/git-pipe ./cmd/git-pipe

local: build/git-pipe

lint:
	golangci-lint run

test:
	go test -v ./...

snapshot: man
	goreleaser --rm-dist --snapshot

assemble-docs: build/git-pipe
	cat _header.md > README.md
	find _docs -name '*.md' -type f | sort | xargs -n 1 sed 's/^#/\n\n##/' | sed 's/\n\n/\n/' >> README.md
	echo '\n\n## Usage\n\n' >> README.md
	stty rows 1024 cols 1024
	./build/git-pipe --help | grep '  ' | sed -n -r '/^\s+[a-z]/p' | tail -n +2 | sed -E 's/^\s+([a-z]+)\s+(.*)?/* `\1` - \2/' >> README.md
	./build/git-pipe --help | grep '  ' | sed -n -r '/^\s+[a-z]/p' | tail -n +2 | awk '{print $$1}' | xargs -n 1 -i sh -c 'echo "\n## {}\n\`\`\`" >> README.md && ./build/git-pipe "{}" --help >> README.md && echo "\`\`\`\n" >> README.md'

README.md: assemble-docs

man: build/git-pipe.1.gz

clean:
	rm -rf build

.PHONY: snapshot lint