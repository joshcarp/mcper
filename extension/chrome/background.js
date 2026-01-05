// MCPER Chrome Bridge - Background Service Worker
// Handles browser automation commands from the native messaging host

const NATIVE_HOST_NAME = 'com.mcper.chrome_bridge';

// Connection state
let nativePort = null;
let pendingRequests = new Map();
let requestId = 0;

// Connect to native messaging host
function connectToNativeHost() {
  if (nativePort) {
    return;
  }

  try {
    nativePort = chrome.runtime.connectNative(NATIVE_HOST_NAME);
    console.log('Connected to native host');

    nativePort.onMessage.addListener((message) => {
      handleNativeMessage(message);
    });

    nativePort.onDisconnect.addListener(() => {
      console.log('Disconnected from native host:', chrome.runtime.lastError?.message);
      nativePort = null;
      // Attempt reconnection after delay
      setTimeout(connectToNativeHost, 5000);
    });
  } catch (error) {
    console.error('Failed to connect to native host:', error);
  }
}

// Handle messages from native host
function handleNativeMessage(message) {
  console.log('Received from native:', message);

  if (message.id && pendingRequests.has(message.id)) {
    // Response to a pending request
    const { resolve } = pendingRequests.get(message.id);
    pendingRequests.delete(message.id);
    resolve(message);
    return;
  }

  // Command from native host
  if (message.command) {
    handleCommand(message).then((result) => {
      sendToNative({ id: message.id, ...result });
    }).catch((error) => {
      sendToNative({ id: message.id, error: error.message });
    });
  }
}

// Send message to native host
function sendToNative(message) {
  if (nativePort) {
    nativePort.postMessage(message);
  } else {
    console.error('Not connected to native host');
  }
}

// Handle automation commands
async function handleCommand(message) {
  const { command, params } = message;

  switch (command) {
    case 'navigate':
      return await navigateToUrl(params);
    case 'click':
      return await clickElement(params);
    case 'type':
      return await typeText(params);
    case 'screenshot':
      return await takeScreenshot(params);
    case 'get_html':
      return await getHtml(params);
    case 'get_text':
      return await getText(params);
    case 'evaluate':
      return await evaluateScript(params);
    case 'list_tabs':
      return await listTabs();
    case 'switch_tab':
      return await switchTab(params);
    case 'new_tab':
      return await newTab(params);
    case 'close_tab':
      return await closeTab(params);
    case 'scroll':
      return await scrollPage(params);
    case 'wait':
      return await waitForSelector(params);
    case 'cdp':
      return await sendCdpCommand(params);
    case 'ping':
      return { success: true, message: 'pong' };
    default:
      return { error: `Unknown command: ${command}` };
  }
}

// Get active tab
async function getActiveTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab) {
    throw new Error('No active tab found');
  }
  return tab;
}

// Navigate to URL
async function navigateToUrl({ url, tabId, waitUntil = 'load' }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  await chrome.tabs.update(tab.id, { url });

  // Wait for page load
  return new Promise((resolve) => {
    const listener = (updatedTabId, changeInfo) => {
      if (updatedTabId === tab.id && changeInfo.status === 'complete') {
        chrome.tabs.onUpdated.removeListener(listener);
        resolve({ success: true, tabId: tab.id, url });
      }
    };
    chrome.tabs.onUpdated.addListener(listener);

    // Timeout after 30 seconds
    setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      resolve({ success: true, tabId: tab.id, url, warning: 'Navigation timeout' });
    }, 30000);
  });
}

// Click element
async function clickElement({ selector, tabId, button = 'left' }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const results = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: (sel, btn) => {
      const element = document.querySelector(sel);
      if (!element) {
        return { error: `Element not found: ${sel}` };
      }

      // Scroll into view
      element.scrollIntoView({ behavior: 'instant', block: 'center' });

      // Create and dispatch click event
      const event = new MouseEvent('click', {
        bubbles: true,
        cancelable: true,
        view: window,
        button: btn === 'right' ? 2 : btn === 'middle' ? 1 : 0
      });
      element.dispatchEvent(event);

      return { success: true, selector: sel };
    },
    args: [selector, button]
  });

  return results[0]?.result || { error: 'Script execution failed' };
}

// Type text into element
async function typeText({ selector, text, tabId, delay = 0 }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const results = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: (sel, txt) => {
      const element = document.querySelector(sel);
      if (!element) {
        return { error: `Element not found: ${sel}` };
      }

      // Focus the element
      element.focus();

      // Set value for input/textarea
      if (element.tagName === 'INPUT' || element.tagName === 'TEXTAREA') {
        element.value = txt;
        element.dispatchEvent(new Event('input', { bubbles: true }));
        element.dispatchEvent(new Event('change', { bubbles: true }));
      } else if (element.isContentEditable) {
        element.textContent = txt;
        element.dispatchEvent(new Event('input', { bubbles: true }));
      }

      return { success: true, selector: sel, text: txt };
    },
    args: [selector, text]
  });

  return results[0]?.result || { error: 'Script execution failed' };
}

// Take screenshot
async function takeScreenshot({ tabId, fullPage = false, format = 'png' }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, {
    format: format === 'jpeg' ? 'jpeg' : 'png',
    quality: format === 'jpeg' ? 80 : undefined
  });

  return { success: true, dataUrl, format };
}

// Get page HTML
async function getHtml({ selector, tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const results = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: (sel) => {
      if (sel) {
        const element = document.querySelector(sel);
        return element ? { success: true, html: element.outerHTML } : { error: `Element not found: ${sel}` };
      }
      return { success: true, html: document.documentElement.outerHTML };
    },
    args: [selector]
  });

  return results[0]?.result || { error: 'Script execution failed' };
}

// Get text content
async function getText({ selector, tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const results = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: (sel) => {
      if (sel) {
        const element = document.querySelector(sel);
        return element ? { success: true, text: element.textContent } : { error: `Element not found: ${sel}` };
      }
      return { success: true, text: document.body.textContent };
    },
    args: [selector]
  });

  return results[0]?.result || { error: 'Script execution failed' };
}

// Evaluate JavaScript
async function evaluateScript({ script, tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const results = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: (code) => {
      try {
        const result = eval(code);
        return { success: true, result: JSON.stringify(result) };
      } catch (error) {
        return { error: error.message };
      }
    },
    args: [script]
  });

  return results[0]?.result || { error: 'Script execution failed' };
}

// List tabs
async function listTabs() {
  const tabs = await chrome.tabs.query({});
  return {
    success: true,
    tabs: tabs.map(t => ({
      id: t.id,
      url: t.url,
      title: t.title,
      active: t.active,
      windowId: t.windowId
    }))
  };
}

// Switch to tab
async function switchTab({ tabId }) {
  await chrome.tabs.update(tabId, { active: true });
  const tab = await chrome.tabs.get(tabId);
  await chrome.windows.update(tab.windowId, { focused: true });
  return { success: true, tabId };
}

// Create new tab
async function newTab({ url }) {
  const tab = await chrome.tabs.create({ url: url || 'about:blank' });
  return { success: true, tabId: tab.id, url: tab.url };
}

// Close tab
async function closeTab({ tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();
  await chrome.tabs.remove(tab.id);
  return { success: true, tabId: tab.id };
}

// Scroll page
async function scrollPage({ x = 0, y = 0, selector, tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  const results = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: (scrollX, scrollY, sel) => {
      if (sel) {
        const element = document.querySelector(sel);
        if (!element) {
          return { error: `Element not found: ${sel}` };
        }
        element.scrollIntoView({ behavior: 'smooth', block: 'center' });
        return { success: true, scrolledTo: sel };
      }
      window.scrollBy(scrollX, scrollY);
      return { success: true, scrolledBy: { x: scrollX, y: scrollY } };
    },
    args: [x, y, selector]
  });

  return results[0]?.result || { error: 'Script execution failed' };
}

// Wait for selector
async function waitForSelector({ selector, timeout = 10000, state = 'visible', tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();
  const startTime = Date.now();

  while (Date.now() - startTime < timeout) {
    const results = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: (sel, targetState) => {
        const element = document.querySelector(sel);
        if (!element) {
          return { found: false };
        }

        if (targetState === 'hidden') {
          const style = window.getComputedStyle(element);
          return { found: style.display === 'none' || style.visibility === 'hidden' };
        }

        if (targetState === 'visible') {
          const rect = element.getBoundingClientRect();
          const style = window.getComputedStyle(element);
          return {
            found: rect.width > 0 && rect.height > 0 &&
                   style.display !== 'none' && style.visibility !== 'hidden'
          };
        }

        // 'attached' - just check if element exists
        return { found: true };
      },
      args: [selector, state]
    });

    if (results[0]?.result?.found) {
      return { success: true, selector, state };
    }

    await new Promise(r => setTimeout(r, 100));
  }

  return { error: `Timeout waiting for selector: ${selector}` };
}

// Send CDP command
async function sendCdpCommand({ method, params = {}, tabId }) {
  const tab = tabId ? await chrome.tabs.get(tabId) : await getActiveTab();

  // Attach debugger if not already attached
  try {
    await chrome.debugger.attach({ tabId: tab.id }, '1.3');
  } catch (e) {
    // Already attached, ignore
  }

  try {
    const result = await chrome.debugger.sendCommand({ tabId: tab.id }, method, params);
    return { success: true, result };
  } catch (error) {
    return { error: error.message };
  }
}

// Listen for messages from popup or content scripts
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'get_status') {
    sendResponse({
      connected: nativePort !== null,
      version: chrome.runtime.getManifest().version
    });
    return true;
  }

  if (message.type === 'command') {
    handleCommand(message).then(sendResponse);
    return true; // Keep channel open for async response
  }
});

// Listen for external messages (from native host HTTP bridge)
chrome.runtime.onMessageExternal.addListener((message, sender, sendResponse) => {
  if (message.command) {
    handleCommand(message).then(sendResponse);
    return true;
  }
});

// Initialize
console.log('MCPER Chrome Bridge initialized');
connectToNativeHost();
