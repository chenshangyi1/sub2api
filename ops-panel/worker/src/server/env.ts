export function appSecret() {
  const secret = process.env.APP_SECRET;
  if (secret) return secret;
  if (process.env.NODE_ENV === "production") throw new Error("APP_SECRET must be set in production");
  return "dev-secret-change-before-production-24";
}
