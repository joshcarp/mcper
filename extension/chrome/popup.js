// MCPER Chrome Bridge - Popup Script

document.addEventListener('DOMContentLoaded', async () => {
  await updateStatus();
  await updateActiveTab();

  document.getElementById('test-connection').addEventListener('click', testConnection);
  document.getElementById('test-screenshot').addEventListener('click', testScreenshot);
});

async function updateStatus() {
  const statusEl = document.getElementById('status');
  const statusText = document.getElementById('status-text');
  const versionEl = document.getElementById('version');

  try {
    const response = await chrome.runtime.sendMessage({ type: 'get_status' });

    versionEl.textContent = response.version || '-';

    if (response.connected) {
      statusEl.className = 'status connected';
      statusText.textContent = 'Connected to native host';
    } else {
      statusEl.className = 'status disconnected';
      statusText.textContent = 'Native host not connected';
    }
  } catch (error) {
    statusEl.className = 'status disconnected';
    statusText.textContent = 'Error: ' + error.message;
  }
}

async function updateActiveTab() {
  const activeTabEl = document.getElementById('active-tab');

  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab) {
      const host = tab.url ? new URL(tab.url).hostname : 'N/A';
      activeTabEl.textContent = host.substring(0, 20) + (host.length > 20 ? '...' : '');
      activeTabEl.title = tab.url;
    }
  } catch (error) {
    activeTabEl.textContent = 'Error';
  }
}

async function testConnection() {
  showResult('Testing connection...', 'success');

  try {
    const response = await chrome.runtime.sendMessage({
      type: 'command',
      command: 'ping'
    });

    if (response.success) {
      showResult('Connection OK!\n\nResponse: ' + JSON.stringify(response, null, 2), 'success');
    } else {
      showResult('Connection failed:\n' + (response.error || 'Unknown error'), 'error');
    }
  } catch (error) {
    showResult('Error: ' + error.message, 'error');
  }
}

async function testScreenshot() {
  showResult('Taking screenshot...', 'success');

  try {
    const response = await chrome.runtime.sendMessage({
      type: 'command',
      command: 'screenshot'
    });

    if (response.success && response.dataUrl) {
      showResult('Screenshot captured!\n\nData URL length: ' + response.dataUrl.length + ' bytes', 'success');
    } else {
      showResult('Screenshot failed:\n' + (response.error || 'Unknown error'), 'error');
    }
  } catch (error) {
    showResult('Error: ' + error.message, 'error');
  }
}

function showResult(message, type) {
  const resultEl = document.getElementById('test-result');
  resultEl.style.display = 'block';
  resultEl.className = 'test-result ' + type;
  resultEl.textContent = message;
}
