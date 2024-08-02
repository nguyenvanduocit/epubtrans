function enableContentEditable() {
    document.querySelectorAll('[data-translation-id]').forEach(element => {
        element.contentEditable = true;
        element.addEventListener('blur', function () {
            console.log(`Element with data-translation-id "${this.dataset.translationId}" updated to: "${this.innerHTML}"`);
            updateTranslateContent(this.dataset.translationId, this.innerHTML);
        });
    });
}

function updateTranslateContent(translationID, translationContent) {
    fetch('/api/translate', {
        method: 'PATCH',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({
            file_path: window.location.pathname,
            translation_id: translationID,
            translation_content: translationContent
        })
    })
        .then(response => response.json())
        .then(data => console.log('Success:', data))
        .catch((error) => console.error('Error:', error));
}

window.onload = function (e) {
    enableContentEditable();
}
