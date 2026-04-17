#!/bin/sh
# Removes Apple developer team ID from the Xcode project before committing.
# After cloning, re-select your team in Xcode: Signing & Capabilities > Team.
sed -i '' 's/DEVELOPMENT_TEAM = .*;/DEVELOPMENT_TEAM = "";/' whitelist-bypass-proxy.xcodeproj/project.pbxproj
echo "Stripped DEVELOPMENT_TEAM from project.pbxproj"
