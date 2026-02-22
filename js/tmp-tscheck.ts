import type { BasePlugin } from '@google/adk';
import { SigilClient, defaultConfig } from './src/index.js';
import { withSigilGoogleAdkPlugins } from './src/frameworks/google-adk/index.js';

const c = new SigilClient(defaultConfig());
const arr: BasePlugin[] = [];
const cfg: {plugins: BasePlugin[]} = withSigilGoogleAdkPlugins({plugins: arr}, c);
console.log(cfg.plugins.length);
