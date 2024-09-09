function enableContentEditable() {
    document.querySelectorAll('[data-translation-id]').forEach(element => {
        const originalContent = element.innerHTML;
        element.contentEditable = true;
        element.addEventListener('blur', function () {
            if (!isTranslating && this.innerHTML !== originalContent) {
            updateTranslateContent(this.dataset.translationId, this.innerHTML);
}
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


function addTranslateButtons() {
    document.querySelectorAll('[data-content-id]').forEach(element => {
        const container = document.createElement('div');
        container.className = 'translate-container';

        const button = document.createElement('button');
        button.textContent = 'Translate';
        button.className = 'translate-button';

        const input = document.createElement('input');
        input.type = 'text';
        input.placeholder = 'Instructions for AI';
        input.className = 'translate-instructions';

        button.addEventListener('click', function() {
            const instructions = input.value;
            translateContent(element.dataset.contentId, element.dataset.translationById, button, instructions);
        });

        container.appendChild(input);
        container.appendChild(button);
        element.parentNode.insertBefore(container, element.nextSibling);
    });
}

let isTranslating = false;


function translateContent(contentId, translationID, button, instructions) {
    // Disable editing
    isTranslating = true;
    const element = document.querySelector(`[data-translation-id="${translationID}"]`);
    element.contentEditable = false;

    // Disable the button and show loading
    button.disabled = true;
    button.textContent = 'Translating...';
    button.classList.add('loading');

    fetch('/api/translate-ai', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({
            file_path: window.location.pathname,
            content_id: contentId,
            translation_id: translationID,
            instructions: instructions
        })
    })
    .then(response => response.json())
    .then(data => {
        if (data.translated_content) {
            const element = document.querySelector(`[data-translation-id="${translationID}"]`);
            element.innerHTML = data.translated_content;
            element.contentEditable = true
        }
    })
    .catch((error) => console.error('Translation Error:', error))
    .finally(() => {
        // Re-enable the button and remove loading state
        button.disabled = false;
        button.textContent = 'Translate';
        button.classList.remove('loading');
        // Re-enable editing
        isTranslating = false;
        element.contentEditable = true;
        element.focus();
    });
}

window.onload = function (e) {
    enableContentEditable();
    addTranslateButtons();
}
