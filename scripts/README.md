# Epub Translator

[![Open In Colab](https://colab.research.google.com/assets/colab-badge.svg)](https://colab.research.google.com/github/nguyenvanduocit/epubtrans/blob/main/scripts/epub-translator-colab.ipynb)

This project aims to quickly translate epub books into bilingual versions, packaging the result as a book with both the original text and the translation. It's designed to maintain the original text format while providing a rough translation.

[Watch the tutorial video (Chinese Simplified)](https://youtu.be/9MspqDLPaxQ)

## Table of Contents
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Usage](#usage)
- [Web Serving](#web-serving)
- [Editing Translations](#editing-translations)
- [Contributing](#contributing)
- [Limitations and Known Issues](#limitations-and-known-issues)

## Quick Start

1. Install Epub Translator (see [Installation](#installation))
2. Set up your ANTHROPIC_KEY:
   ```bash
   export ANTHROPIC_KEY=your_anthropic_key
   ```
3. Translate a book:
   ```bash
   epubtrans unpack /path/to/book.epub
   epubtrans clean /path/to/unpacked-epub
   epubtrans mark /path/to/unpacked-epub
   epubtrans translate /path/to/unpacked-epub --source English --target Chinese Simplified
   epubtrans pack /path/to/unpacked
   ```

## Installation

### Prerequisites
- Windows: PowerShell 5.1 or later
- Linux/macOS: Bash shell
- All systems: Internet connection

### Windows

1. Open PowerShell as Administrator and run:

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

Open a terminal and run:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/nguyenvanduocit/epubtrans/main/scripts/install_unix.sh)"
```

### Verify Installation

After installation, verify by running:

```
epubtrans --version
```

## Usage

### Available Commands

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
  serve       Serve the content of an unpacked EPUB as a web server
  styling     Style the content of an unpacked EPUB
  translate   Translate the content of an unpacked EPUB
  unpack      Unpack a book
  upgrade     Self update the tool

Flags:
  -h, --help      help for epubtrans
  -v, --version   version for epubtrans
```

### Step-by-step Guide

0. Configure environment:
   ```bash
   export ANTHROPIC_KEY=your_anthropic_key
   ```
   Note: You need to obtain an ANTHROPIC_KEY from Anthropic's website to use their translation API.

1. Unpack the epub file:
   ```bash
   epubtrans unpack /path/to/file.epub
   ```

2. Clean up HTML files:
   ```bash
   epubtrans clean /path/to/unpacked-epub
   ```

3. Mark content for translation:
   ```bash
   epubtrans mark /path/to/unpacked-epub
   ```

4. Translate marked content:
   ```bash
   epubtrans translate /path/to/unpacked-epub --source English --target Chinese Simplified
   ```

5. (Optional) Apply styling:
   ```bash
   epubtrans styling /path/to/unpacked --hide "source|target"
   ```

the command also make original text to be faded out a little bit, so that the translated text can be more visible.

6. Package into a bilingual book:
   ```bash
   epubtrans pack /path/to/unpacked
   ```

## Web Serving

To serve the book on the web:

```bash
epubtrans serve /path/to/unpacked
```

Important endpoints:
- http://localhost:8080/api/info
- http://localhost:8080/toc.html
- http://localhost:3000/api/manifest
- http://localhost:3000/api/spine

## Editing Translations

When accessing the book via the `serve` command, the translated content is editable. After editing, the content is automatically saved when you move the mouse away.

To apply changes, run the `pack` command again.

[Watch the editing tutorial video](https://youtu.be/XKIj-gyHgmI)

## Contributing

We welcome contributions to the Epub Translator project! Here's how you can help:

1. Fork the repository
2. Create a new branch for your feature or bug fix
3. Make your changes and commit them
4. Push to your fork and submit a pull request

Please ensure your code adheres to the project's coding standards and include tests for new features.

## Support the Project

If you find Epub Translator useful, please consider supporting the project financially. Your contributions help maintain and improve the tool, ensuring its continued development and availability.

You can support the project in the following ways:

- [PayPal](https://paypal.me/duocnguyen)

## Limitations and Known Issues

- The quality of translation depends on the Anthropic API and may not be perfect for all types of content or language pairs.
- Large books may take a considerable amount of time to translate.

For any issues or feature requests, please open an issue on the GitHub repository.
