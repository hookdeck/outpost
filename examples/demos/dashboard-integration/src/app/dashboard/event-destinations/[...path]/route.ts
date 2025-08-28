import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { getPortalUrl } from "@/lib/outpost";
import logger from "@/lib/logger";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  try {
    // Validate session
    const token = await getToken({
      req: request,
      secret: process.env.NEXTAUTH_SECRET,
    });

    if (!token?.id) {
      return NextResponse.redirect(new URL("/auth/login", request.url));
    }

    // Extract and validate the path
    const resolvedParams = await params;
    const pathSegments = resolvedParams.path || [];
    const portalPath =
      pathSegments.length > 0 ? "/" + pathSegments.join("/") : "";

    // Validate the path - allow /new and /destinations/{uuid}
    const isNewRoute = portalPath === "/new";
    const isDestinationRoute = !!portalPath.match(
      /^\/destinations\/[a-f0-9\-]{36}$/,
    );
    const isValidPath = isNewRoute || isDestinationRoute;

    if (!isValidPath) {
      logger.debug("Invalid portal path, redirecting to root", { portalPath });
      return NextResponse.redirect(
        new URL("/dashboard/event-destinations", request.url),
      );
    }

    // Get theme parameter
    const theme = request.nextUrl.searchParams.get("theme") || "light";

    // Generate portal URL using Outpost SDK (user ID = tenant ID)
    const tenantId = token.id as string;
    const portalUrl = await getPortalUrl(tenantId, theme);

    // Append the portal path to the portal URL
    const finalUrl = new URL(portalUrl);

    if (portalPath) {
      // Handle path construction carefully to avoid double slashes
      const basePath = finalUrl.pathname.endsWith("/")
        ? finalUrl.pathname.slice(0, -1)
        : finalUrl.pathname;
      finalUrl.pathname = basePath + portalPath;
    }

    // Redirect to portal
    return NextResponse.redirect(finalUrl.toString());
  } catch (error) {
    logger.error("Portal redirect error", { error });
    return NextResponse.json(
      { error: "Failed to access event destinations" },
      { status: 500 },
    );
  }
}
