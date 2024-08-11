# Epub Translator

This project aims to quickly translate epub books into Vietnamese. It packages the result as a bilingual book.

You may want to watch the [tutorial video - Vietnamese](https://youtu.be/9MspqDLPaxQ).

## Acceptance Criteria

- [x] Only need to create a rough translation.
- [x] Maintain the format of the original text.

## Installation

1. Install Go. You can download it from [here](https://golang.org/dl/).
2. Install the tool

```bash
go install github.com/nguyenvanduocit/epubtrans@latest
```

## Usage

To manage the translation content, we need to mark the content that needs to be translated, then translate and mark the
translated content. We divide it into 3 commands.

1. `unpack` to extract the epub file.
2. `mark` to mark the content that needs to be translated.
3. `clean` to clean up erroneous content.
4. `translate` to translate the marked content.
5. `pack` to package it into a bilingual book.
6. `serve` to serve the whole book as a static webserver.

### Step-by-step

All commands take the path to the epub file to be translated as the first parameter.

0. Config env

```bash
export ANTHROPIC_KEY=your_anthropic_key
```

1. Unpack the epub file.

 ```bash
epubtrans unpack /path/to/file.epub
 ```

2. Clean up html files.

 ```bash
epubtrans clean /path/to/unpacked
 ```

3. Mark the content that needs to be translated.

 ```bash
epubtrans mark /path/to/unpacked
 ```

At this point, you will see a folder with the name of the epub, containing the html files of the epub. The content of
these html files has been marked.

4. Translate the marked content.

 ```bash
epubtrans translate /path/to/unpacked
 ```

This process will take some time. At the end of the process, you will have html files with the translated content.

5. Apply style to the translated content.

The step is optional. Only when you want to apply some style to the translated content.

 ```bash
epubtrans styling /path/to/unpacked --hide "source|target"
 ```

6. Package it into a bilingual book.

 ```bash
epubtrans pack /path/to/unpacked
 ```

At this point, you will have a repacked epub file with bilingual content.

## How to serve the book on web?

You can run the command `serve`. then the console will show you the address to access the book.

There are some important endpoints:

- http://localhost:8080/api/info
- http://localhost:8080/toc.html
- http://localhost:3000/api/manifest
- http://localhost:3000/api/spine

### How to edit the translation?

When accessing the book via `serve` command, you can see that the translated content is editable. After edit and leave the mouse, the content will be saved automatically.

After that, you just need to run the `pack` command to package the book again.

## Snippets

### How to hide all English content?

You need to add the following CSS to every html file or css file:

```css
[data-content-id] {
    display: none !important;
}
```

### How to make original content less visible?

You need to add the following CSS to every html file or css file:

```css
[data-content-id] {
    opacity: 0.8;
}
```
