install:
	rm -rf $(which epubtrans)
	go install ./...
