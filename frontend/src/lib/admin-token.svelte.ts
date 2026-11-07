// The superadmin token for /admin. It lives in sessionStorage, so it is gone
// when the tab closes, and in reactive state, so locking (by the Lock button or
// after a 401) swaps the page back to the token gate immediately.
// Storage can be unavailable (private mode, blocked site data): every access
// is guarded and the session then lasts only as long as the page.

const KEY = 'leadintel.admin-token';

function read(): string | null {
	try {
		return sessionStorage.getItem(KEY);
	} catch {
		return null;
	}
}

function write(token: string | null) {
	try {
		if (token === null) sessionStorage.removeItem(KEY);
		else sessionStorage.setItem(KEY, token);
	} catch {
		// Storage unavailable: the in-memory value still works for this page.
	}
}

let token = $state<string | null>(read());

export const adminSession = {
	get token() {
		return token;
	},
	unlock(value: string) {
		token = value;
		write(value);
	},
	lock() {
		token = null;
		write(null);
	}
};
