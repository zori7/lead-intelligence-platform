// Applies the saved colour mode before the app renders, so a light-mode user
// does not see a dark flash. Runs as a file (not inline) to keep a strict CSP
// possible. Key and values are mode-watcher's ("light" | "dark" | "system").
//
// With nothing saved, the mode is seeded as "dark" here: mode-watcher persists
// its own fallback ("system") when its module loads, before the <ModeWatcher>
// defaultMode prop is read, so the default has to be in storage first.
(function () {
	var root = document.documentElement;
	try {
		var key = 'mode-watcher-mode';
		var mode = localStorage.getItem(key);
		if (mode !== 'light' && mode !== 'dark' && mode !== 'system') {
			mode = 'dark';
			localStorage.setItem(key, mode);
		}
		var dark =
			mode === 'dark' ||
			(mode === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
		root.classList.toggle('dark', dark);
	} catch (e) {
		// Storage unavailable: keep the default (dark) from app.html.
	}
})();
