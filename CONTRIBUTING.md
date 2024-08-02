# Contributing to [Project Name]

We welcome contributions to [Project Name]! This document outlines the process for contributing to this project and how to get started.

## Getting Started

1. Fork the repository on GitHub.
2. Clone your fork locally:
   ```
   git clone https://github.com/nguyenvanduocit/epubtrans.git
   ```
3. Create a new branch for your feature or bug fix:
   ```
   git checkout -b feature/your-feature-name
   ```

## Development Environment

1. Ensure you have Go installed (version X.X or higher).
2. Install any necessary dependencies:
   ```
   go mod tidy
   ```

## Making Changes

1. Make your changes in your feature branch.
2. Add or update tests as necessary.
3. Ensure all tests pass:
   ```
   go test ./...
   ```
4. Run `gofmt` to ensure your code follows Go's formatting standards:
   ```
   gofmt -s -w .
   ```

## Committing Changes

1. Commit your changes with a clear and descriptive commit message:
   ```
   git commit -m "Add feature X" -m "This feature does Y and Z"
   ```
2. Push your changes to your fork:
   ```
   git push origin feature/your-feature-name
   ```

## Submitting a Pull Request

1. Go to the original repository on GitHub.
2. Click the "New pull request" button.
3. Select your feature branch from your fork.
4. Fill out the pull request template with all relevant information.
5. Submit the pull request.

## Code Review Process

1. Maintainers will review your pull request.
2. They may ask for changes or clarifications.
3. Make any requested changes in your feature branch and push the changes.
4. Once approved, a maintainer will merge your pull request.

## Coding Standards

- Follow Go's official [Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).
- Write clear, readable, and maintainable code.
- Document your code using Go's standard commenting practices.

## Reporting Issues

- Use the GitHub issue tracker to report bugs.
- Clearly describe the issue, including steps to reproduce.
- Include your Go version and operating system.

Thank you for contributing to Epubtrans
