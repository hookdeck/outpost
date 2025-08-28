import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { getPortalUrl } from "@/lib/outpost";
import logger from "@/lib/logger";

export async function GET(request: NextRequest) {
  try {
    // Validate session
    const token = await getToken({
      req: request,
      secret: process.env.NEXTAUTH_SECRET,
    });

    if (!token?.id) {
      return NextResponse.redirect(new URL("/auth/login", request.url));
    }

    // Get theme parameter
    const theme = request.nextUrl.searchParams.get("theme") || "light";

    // Generate portal URL using Outpost SDK (user ID = tenant ID)
    const tenantId = token.id as string;
    const portalUrl = await getPortalUrl(tenantId, theme);

    // Redirect to portal (root path)
    return NextResponse.redirect(portalUrl);
  } catch (error) {
    logger.error("Portal redirect error", { error });
    return NextResponse.json(
      { error: "Failed to access event destinations" },
      { status: 500 },
    );
  }
}
