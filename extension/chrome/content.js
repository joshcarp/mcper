// MCPER Chrome Bridge - Content Script
// Injected into pages for DOM interaction

// Listen for messages from background script
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'dom_action') {
    handleDomAction(message).then(sendResponse);
    return true;
  }
});

async function handleDomAction(message) {
  const { action, params } = message;

  switch (action) {
    case 'query':
      return queryElement(params);
    case 'query_all':
      return queryAllElements(params);
    case 'get_attributes':
      return getElementAttributes(params);
    case 'get_computed_style':
      return getElementComputedStyle(params);
    case 'highlight':
      return highlightElement(params);
    case 'get_bounding_rect':
      return getElementBoundingRect(params);
    default:
      return { error: `Unknown DOM action: ${action}` };
  }
}

function queryElement({ selector }) {
  const element = document.querySelector(selector);
  if (!element) {
    return { found: false, selector };
  }
  return {
    found: true,
    selector,
    tagName: element.tagName,
    id: element.id,
    className: element.className,
    textContent: element.textContent?.substring(0, 200),
    innerHTML: element.innerHTML?.substring(0, 500)
  };
}

function queryAllElements({ selector, limit = 10 }) {
  const elements = document.querySelectorAll(selector);
  const results = [];

  for (let i = 0; i < Math.min(elements.length, limit); i++) {
    const el = elements[i];
    results.push({
      index: i,
      tagName: el.tagName,
      id: el.id,
      className: el.className,
      textContent: el.textContent?.substring(0, 100)
    });
  }

  return {
    found: elements.length > 0,
    count: elements.length,
    elements: results
  };
}

function getElementAttributes({ selector }) {
  const element = document.querySelector(selector);
  if (!element) {
    return { error: `Element not found: ${selector}` };
  }

  const attributes = {};
  for (const attr of element.attributes) {
    attributes[attr.name] = attr.value;
  }

  return { success: true, attributes };
}

function getElementComputedStyle({ selector, properties }) {
  const element = document.querySelector(selector);
  if (!element) {
    return { error: `Element not found: ${selector}` };
  }

  const computed = window.getComputedStyle(element);
  const styles = {};

  if (properties && properties.length > 0) {
    for (const prop of properties) {
      styles[prop] = computed.getPropertyValue(prop);
    }
  } else {
    // Return common properties
    const common = ['display', 'visibility', 'position', 'width', 'height',
                    'color', 'backgroundColor', 'fontSize', 'fontFamily'];
    for (const prop of common) {
      styles[prop] = computed.getPropertyValue(prop);
    }
  }

  return { success: true, styles };
}

function highlightElement({ selector, duration = 2000 }) {
  const element = document.querySelector(selector);
  if (!element) {
    return { error: `Element not found: ${selector}` };
  }

  const originalOutline = element.style.outline;
  const originalBackground = element.style.backgroundColor;

  element.style.outline = '3px solid red';
  element.style.backgroundColor = 'rgba(255, 0, 0, 0.1)';

  setTimeout(() => {
    element.style.outline = originalOutline;
    element.style.backgroundColor = originalBackground;
  }, duration);

  return { success: true, selector };
}

function getElementBoundingRect({ selector }) {
  const element = document.querySelector(selector);
  if (!element) {
    return { error: `Element not found: ${selector}` };
  }

  const rect = element.getBoundingClientRect();
  return {
    success: true,
    rect: {
      x: rect.x,
      y: rect.y,
      width: rect.width,
      height: rect.height,
      top: rect.top,
      right: rect.right,
      bottom: rect.bottom,
      left: rect.left
    }
  };
}

// Announce content script loaded
console.log('MCPER Chrome Bridge content script loaded');
