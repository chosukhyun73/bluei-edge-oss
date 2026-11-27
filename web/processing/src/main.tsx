import { createRoot } from 'react-dom/client';
import './styles/globals.css';
import App from './App';
import { LanguageProvider } from './lib/language-context';

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('#root element not found');

createRoot(rootEl).render(
  <LanguageProvider>
    <App />
  </LanguageProvider>,
);
