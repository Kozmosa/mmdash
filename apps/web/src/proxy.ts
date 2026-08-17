import { type NextRequest, NextResponse } from "next/server";

const protectedPrefixes = ["/projects", "/account", "/cli/authorize"];

export function proxy(request: NextRequest) {
  const pathname = request.nextUrl.pathname;
  const hasSession = request.cookies.has("mmdash_session");

  // Public entry pages should not be reachable once a browser session exists.
  // Keep this check in the proxy so the login form is never rendered for an
  // already authenticated user (including on a direct navigation to /login).
  if (
    hasSession &&
    (pathname === "/" || pathname === "/login" || pathname === "/register")
  ) {
    return NextResponse.redirect(new URL("/projects", request.url));
  }
  if (!protectedPrefixes.some((prefix) => pathname.startsWith(prefix)))
    return NextResponse.next();
  if (hasSession) return NextResponse.next();
  const login = new URL("/login", request.url);
  login.searchParams.set("returnTo", `${pathname}${request.nextUrl.search}`);
  return NextResponse.redirect(login);
}

export const config = {
  matcher: [
    "/",
    "/login",
    "/register",
    "/projects/:path*",
    "/account/:path*",
    "/cli/authorize/:path*",
  ],
};
