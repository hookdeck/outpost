import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { getTenantOverview } from "../../../lib/outpost";
import logger from "../../../lib/logger";

export async function GET(request: NextRequest) {
  try {
    // Validate session using JWT token
    const token = await getToken({
      req: request,
      secret: process.env.NEXTAUTH_SECRET,
    });

    if (!token?.id) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    // Use the user ID directly as the tenant ID
    const tenantId = token.id as string;

    // Get tenant overview from Outpost
    const overview = await getTenantOverview(tenantId);

    return NextResponse.json(overview);
  } catch (error) {
    logger.error("Overview API error", { error });

    return NextResponse.json(
      { error: "Failed to fetch overview data" },
      { status: 500 },
    );
  }
}
