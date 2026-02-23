import './style.css'
import App from './App.svelte'

// --- Native App Behavior Enforcement ---

// 1. Disable Right-Click Context Menu
window.addEventListener('contextmenu', (e) => {
  e.preventDefault();
}, false);

// 2. Disable Middle-Click (Scroll Wheel Click) Auto-Scrolling
window.addEventListener('mousedown', (e) => {
  if (e.button === 1) {
    e.preventDefault();
    return false;
  }
}, true);

// 3. Disable Common Browser Shortcuts (Refresh, Print, etc.)
window.addEventListener('keydown', (e) => {
  if (
    // Refresh (F5 or Ctrl+R)
    (e.key === 'F5') ||
    (e.ctrlKey && (e.key === 'r' || e.key === 'R')) ||
    // Print (Ctrl+P)
    (e.ctrlKey && (e.key === 'p' || e.key === 'P')) ||
    // Save (Ctrl+S)
    (e.ctrlKey && (e.key === 's' || e.key === 'S')) ||
    // Find (Ctrl+F)
    (e.ctrlKey && (e.key === 'f' || e.key === 'F'))
  ) {
    e.preventDefault();
  }
}, false);

const app = new App({
  target: document.getElementById('app')
})

export default app
