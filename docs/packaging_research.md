# Ubuntu Community Preference: Flatpak, Snap, and AppImage

This document outlines the standard Ubuntu community and ecosystem preferences regarding the three major universal Linux packaging formats: Snap, Flatpak, and AppImage.

## 1. Canonical's Official Stance & Snapcraft
Canonical, the company behind Ubuntu, is the creator of **Snap**. Their official stance heavily favors Snap as the premier universal package manager. 
- **Universal Scope:** Snaps are designed for a broad range of use cases, including IoT devices, server applications, background daemons, and desktop applications. 
- **Centralized Control:** The Snap backend (the Snap Store) is [proprietary and closed-source](https://snapcraft.io/docs/snap-store). Canonical maintains exclusive control over the repository backend, meaning independent Snap repositories cannot be hosted by third parties.
- **Default Inclusion:** Canonical mandates that certain core applications (most notably the Firefox web browser) are installed as Snaps by default on Ubuntu systems.

## 2. Ubuntu Flavor Policies regarding Flatpak
While **Flatpak** is widely embraced by the broader Linux community (e.g., Fedora, Linux Mint, Pop!_OS) as the standard for desktop sandboxed applications, its status within the official Ubuntu ecosystem has been deliberately curtailed out-of-the-box.
- **Removal of Default Support:** Starting with the Ubuntu 23.04 (Lunar Lobster) release, Canonical and the teams behind all official Ubuntu flavors (such as Kubuntu, Ubuntu MATE, Xubuntu, etc.) agreed to [stop including Flatpak and its software center integrations by default](https://discourse.ubuntu.com/t/ubuntu-flavor-packaging-defaults/34061). 
- **Rationale:** The stated goal on the Ubuntu Discourse was to "improve the out-of-the-box Ubuntu experience for new users" by providing a singular, cohesive ecosystem focused strictly on `.deb` and `snap` packages, thereby reducing fragmentation.
- **Availability:** Flatpak is not banned; it remains available in the Ubuntu repositories. Users must manually install `flatpak` and `gnome-software-plugin-flatpak` if they wish to use Flathub.

## 3. Flathub vs. Snapcraft Statistics & Policies
- **Flathub (Flatpak):** Flathub is a community-governed, decentralized platform. Its backend is fully open-source. Flathub provides transparent and publicly visible download statistics for its applications on its website. 
- **Snapcraft (Snap):** Snapcraft is centrally governed by Canonical. Rather than exposing cumulative public download numbers, the Snap Store relies on a "Weekly Active Devices" metric, which is primarily restricted to the developers of the specific app rather than being publicly broadcast. 

## 4. AppImage on Ubuntu
**AppImage** takes a different approach by offering standalone, portable executables that do not require a dedicated package manager or daemon.
- **Out-of-the-box Breakage (22.04+):** Historically, AppImages worked immediately on Ubuntu. However, starting with Ubuntu 22.04 LTS, Ubuntu transitioned away from `libfuse2` by default in favor of `fuse3`. 
- **Current State:** Because AppImage relies on `libfuse2` to mount its images, [AppImages fail to launch on fresh Ubuntu 22.04 and 24.04 installations](https://docs.appimage.org/user-guide/troubleshooting/fuse.html). Users encounter a `dlopen(): error loading libfuse.so.2` error and must manually run `sudo apt install libfuse2` (or `libfuse2t64` on 24.04+) to restore functionality. 

## Summary
The "standard" Ubuntu ecosystem preference is heavily engineered from the top down by Canonical to prioritize **Snaps**. While **Flatpak** and **AppImage** are popular among desktop Linux users and can be used on Ubuntu, Canonical has actively removed out-of-the-box support for them to consolidate the Ubuntu experience around Snaps and standard Debian packages.
