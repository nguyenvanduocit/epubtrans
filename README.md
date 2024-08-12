# Epub Translator

This project aims to quickly translate epub books into Vietnamese. It packages the result as a bilingual book.

You may want to watch the [tutorial video - Vietnamese](https://youtu.be/9MspqDLPaxQ).

## Acceptance Criteria

- [x] Only need to create a rough translation.
- [x] Maintain the format of the original text.

## Installation

This guide provides instructions for installing the latest version of epubtrans on Windows, Linux, and macOS.

### Prerequisites

- Windows: PowerShell 5.1 or later
- Linux/macOS: Bash shell
- All systems: Internet connection to download the latest release

### Windows

1. Open PowerShell as Administrator.
2. Run the following commands:

```powershell
$ErrorActionPreference = "Stop"
$version = (Invoke-RestMethod "https://api.github.com/repos/nguyenvanduocit/epubtrans/releases/latest").tag_name
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$url = "https://github.com/nguyenvanduocit/epubtrans/releases/download/${version}/epubtrans_${version.Substring(1)}_windows_${arch}.tar.gz"
Invoke-WebRequest -Uri $url -OutFile "epubtrans.tar.gz"
tar -xzf epubtrans.tar.gz
Move-Item -Force epubtrans.exe "C:\Windows\System32\"
Remove-Item epubtrans.tar.gz
Write-Host "epubtrans $version has been installed successfully!"
```

### Linux and macOS

Open a terminal and run the following command:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/nguyenvanduocit/epubtrans/main/scripts/install_unix.sh)"
```

After installation, verify that epubtrans was installed correctly by opening a new terminal or command prompt and running:

```
epubtrans --version
```

This should display the version number of the installed epubtrans.

## Usage

```
Usage:
  epubtrans [flags]
  epubtrans [command]

Available Commands:
  clean       Clean the html files
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  mark        Mark content in EPUB files
  pack        Zip files in a directory
  serve       serve the content of an unpacked EPUB as a web server
  styling     styling the content of an unpacked EPUB
  translate   Translate the content of an unpacked EPUB
  unpack      unpack a book
  upgrade     Self update the tool

Flags:
  -h, --help      help for epubtrans
  -v, --version   version for epubtrans

Use "epubtrans [command] --help" for more information about a command.
```

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
epubtrans clean /path/to/unpacked-epub
 ```

3. Mark the content that needs to be translated.

 ```bash
epubtrans mark /path/to/unpacked-epub
 ```

At this point, you will see a folder with the name of the epub, containing the html files of the epub. The content of
these html files has been marked.

4. Translate the marked content.

 ```bash
epubtrans translate /path/to/unpacked-epub --source English --target Vietnamese
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
