export async function ensureValidToken(endpoint, storageKey, authHeader) {
  try {
    const token = localStorage.getItem(storageKey);
    if (!token) {
      const newToken = await refreshToken(endpoint, storageKey, authHeader);
      localStorage.setItem(storageKey, newToken);
      return newToken;
    }

    const isValid = await validateToken(endpoint, token);
    if (!isValid) {
      const newToken = await refreshToken(endpoint, storageKey, authHeader);
      localStorage.setItem(storageKey, newToken);
      return newToken;
    }
    return token;
  } catch (error) {
    console.error("Failed to manage authentication token:", error);
    throw error;
  }
}

export async function validateToken(endpoint, token) {
  const response = await fetch(`${endpoint}/test-bearer-token`, {
    method: "GET",
    headers: {
      Authorization: token,
    },
  });
  return response.ok;
}

export async function refreshToken(endpoint, storageKey, authHeader) {
  const response = await fetch(`${endpoint}/token`, {
    method: "POST",
    headers: {
      Authorization: authHeader,
    },
  });

  if (!response.ok) {
    throw new Error("Failed to refresh authentication token");
  }

  const { token } = await response.json();
  localStorage.setItem(storageKey, token);
  return token;
}
