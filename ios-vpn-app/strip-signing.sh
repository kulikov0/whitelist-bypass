#!/bin/sh
set -e
cd "$(dirname "$0")"
PBXPROJ=whitelist-bypass-vpn.xcodeproj/project.pbxproj
sed -i '' 's/DEVELOPMENT_TEAM = .*;/DEVELOPMENT_TEAM = "";/g' "$PBXPROJ"
sed -i '' 's/PRODUCT_BUNDLE_IDENTIFIER = "com\.example\.whitelist-bypass-vpn\.PacketTunnel";/PRODUCT_BUNDLE_IDENTIFIER = "com.example.whitelist-bypass-vpn.PacketTunnel";/g' "$PBXPROJ"
sed -i '' 's/PRODUCT_BUNDLE_IDENTIFIER = "com\.example\.whitelist-bypass-vpn";/PRODUCT_BUNDLE_IDENTIFIER = "com.example.whitelist-bypass-vpn";/g' "$PBXPROJ"
echo "Cleared DEVELOPMENT_TEAM; bundle ids reset to com.example.whitelist-bypass-vpn(.PacketTunnel)."
echo "After cloning, set your own Team and unique bundle ids in Xcode > Signing & Capabilities (app + PacketTunnel)."
