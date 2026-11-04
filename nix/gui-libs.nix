# What GLFW links, and also what it only opens with dlopen when it opens the
# window: the runpath and LD_LIBRARY_PATH need both.
{
  lib,
  libGL,
  libx11,
  libxau,
  libxcb,
  libxcursor,
  libxext,
  libxfixes,
  libxi,
  libxinerama,
  libxrandr,
  libxrender,
  libxxf86vm,
  libxkbcommon,
  wayland,
  libdecor,
  xorgproto,
  wayland-protocols,
}:
rec {
  libs = [
    libGL
    libx11
    libxcursor
    libxext
    libxi
    libxinerama
    libxrandr
    libxrender
    libxxf86vm
    libxkbcommon
    wayland
    libdecor
  ];

  protocols = [
    xorgproto
    wayland-protocols
  ];

  # Every header folder cgo needs, propagated inputs included, for a shell
  # without the stdenv that would bring those in.
  headers =
    map lib.getDev libs
    ++ protocols
    ++ map lib.getDev [
      libxfixes
      libxcb
      libxau
    ];
}
