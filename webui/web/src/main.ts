import { mount } from 'svelte';
import App from './App.svelte';
import './../style.css';

const target = document.getElementById('app');
if (!target) throw new Error('catacomb: #app mount point missing');

export default mount(App, { target });
