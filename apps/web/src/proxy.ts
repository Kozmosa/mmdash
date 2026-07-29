import { type NextRequest, NextResponse } from "next/server";

const protectedPrefixes = ["/projects", "/account"];

export function proxy(request: NextRequest) {
  if (
    !protectedPrefixes.some((prefix) =>
      request.nextUrl.pathname.startsWith(prefix),
    )
  )
    return NextResponse.next();
  if (request.cookies.has("mmdash_session")) return NextResponse.next();
  const login = new URL("/login", request.url);
  login.searchParams.set(
    "returnTo",
    `${request.nextUrl.pathname}${request.nextUrl.search}`,
  );
  return NextResponse.redirect(login);
}

export const config = { matcher: ["/projects/:path*", "/account/:path*"] };
