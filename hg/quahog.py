# Copyright 2025 Google LLC
# SPDX-License-Identifier: GPL-2.0

"""Quahog mercurial extension for managing patches.

This extension helps manage Quilt-style local patch sets::

  path/to/import
  └── patches
      ├── series
      └── patch1.diff

Quahog models each patch as a commit which allows editing, rebasing, reordering,
and all the other operations mercurial supports.

One key feature of this model is that you can rebase a patch set onto a new
version of the patched library with a few easy commands::

  $ hg qu-pop --all --root path/to/import
  $ <import new version>
  $ hg commit -m '#QUAHOG Import new path/to/import version.'
  $ hg rebase --from <first-patch-rev> --to .
  $ hg qu-fold --all --root path/to/import

To enable the extension, add the following to the `extensions` section of your
`.hgrc`::

  [extensions]
  quahog =
"""
from __future__ import absolute_import

import collections
import contextlib
import os
import re
import subprocess
import textwrap

from hgext import rebase
from mercurial import cmdutil
from mercurial import error
from mercurial import extensions
from mercurial import hg
from mercurial import logcmdutil
from mercurial import merge
from mercurial import narrowspec
from mercurial import obsutil
from mercurial import patch
from mercurial import pathutil
from mercurial import phases
from mercurial import pycompat
from mercurial import registrar
from mercurial import rewriteutil
from mercurial import scmutil
from mercurial.util import safehasattr

cmdtable = {}
command = registrar.command(cmdtable)

PATCHDESC_RE = re.compile(rb'(do not submit\s*)?\[patch\]([^\r\n]+)',
                          re.IGNORECASE)


def _separatepatchdescription(patchcontent):
  patchlines = patchcontent.splitlines(keepends=True) + [b'']
  desclines = []
  i = 0
  for i, line in enumerate(patchlines):
    if line.startswith((b'--- ', b'diff --git', b'rename from')):
      break
    if line.startswith(b'Index: '):
      i += 2
      break
    desclines.append(line.rstrip())
  return b''.join(patchlines[i:]).lstrip(), b'\n'.join(desclines).rstrip()


def _canonicalizepatchcontent(patchcontent, rootpath):
  abspathpattern = br'(^|\n)(---|\+\+\+)(\s)(\S*/)?(' + rootpath + br')'
  return re.sub(abspathpattern, br'\1\2\3\4', patchcontent)


def _basiccommitfunc(ui, repo, message, match, opts):
  del ui
  return repo.commit(
      message,
      opts.get(b'user'),
      opts.get(b'date'),
      match,
      editor=None,
      extra={},
  )


def _hgdifftopatch(diff, root):
  lines = []
  rootbasepath = root.rstrip(b'/')
  # Avoid accidentally removing a trailing newline if it was present by
  # retaining all newlines when splitting and joining without a joining
  # character.
  for line in diff.splitlines(keepends=True):
    if line.startswith(b'diff --git'):
      # Skip these lines entirely as Quilt's default diff generation doesn't
      # include them (as it doesn't use git to compute diffs)
      continue
    # Quilts '-p ab' flag creates diffs which don't contain the Quilt root
    # directory, and instead use a/ and b/ for the root of the base and changed
    # file respectively.
    elif line.startswith(b'--- ' + rootbasepath):
      out = b'--- a' + line.removeprefix(b'--- ' + rootbasepath)
    elif line.startswith(b'+++ ' + rootbasepath):
      out = b'+++ b' + line.removeprefix(b'+++ ' + rootbasepath)
    elif line.startswith(b'@@ '):
      # Quilt generates diffs that strip trailing whitespace from context lines.
      # Fig's diffs keep the trailing whitespace by default, so we remove that
      # here. This seems to only be configured for context lines, not all lines.
      ending = line[-2:] if line[-2:] == '\r\n' else line[-1:]
      out = line.rstrip() + ending
    else:
      out = line
    lines.append(out)
  return b''.join(lines)


def _fold_changesets(txn, repo, revs):
  commitopts = {b'message': repo[revs[0]].description(), b'edit': False}
  revs.sort()
  evolvemod = extensions.find(b'evolve')
  root, head, p2 = evolvemod.rewriteutil.foldcheck(repo, revs)
  allctx = [repo[r] for r in revs]
  newid, _ = evolvemod.rewriteutil.rewrite(
      repo,
      root,
      head, [root.p1().node(), p2.node()],
      commitopts=commitopts)
  phases.retractboundary(repo, txn, phases.draft, [newid])
  replacements = {tuple(ctx.node() for ctx in allctx): [newid]}
  scmutil.cleanupnodes(repo, replacements, operation=b'fold')
  return newid


def _ensurenarrowspec(ui, repo, rootpath):
  with repo.wlock(), repo.lock(), repo.transaction(
      b'expandnarrowspec'
  ), repo.dirstate.changing_parents(repo):
    includes, excludes = repo.narrowpats
    adds = {
        b'rootfilesin:' + dirpath
        for dirpath, _, _ in repo.wvfs.walk(rootpath)
    }
    if adds < includes:
      return
    ui.status(b'tracking paths for "%s"\n' % (rootpath,))
    repo.setnarrowpats(includes | adds, excludes)
    narrowspec.updateworkingcopy(repo)
    narrowspec.copytoworkingcopy(repo)


def _trackedroot(ui, repo, root):
  rootpath = pathutil.canonpath(repo.root, repo.dirstate.getcwd(), root or b'.')
  if not root:
    ui.status(b'inferring --root as "%s"\n' % (rootpath,))
  if not repo.wvfs.isdir(rootpath):
    raise error.Abort(b'%s: directory not found' % (rootpath,))
  if not repo.wvfs.isdir(repo.wvfs.reljoin(rootpath, b'patches')):
    raise error.Abort(b'%s: does not contain patches/ subdirectory' % (rootpath,))
  _ensurenarrowspec(ui, repo, rootpath)
  if safehasattr(ui, 'reporting_record_meta'):
    ui.reporting_record_meta(b'quahog.root', rootpath)
  return rootpath


def _isquahogchange(node):
  return b'#QUAHOG' in node.description().upper()


def _isquahogpatch(node):
  m = PATCHDESC_RE.fullmatch(node.description().splitlines()[0])
  return m is not None


def _reverr(node, message):
  maybex = b'x' if node.hex()[0] in b'0123456789' else b''
  return b'rev %s%s: %s' % (maybex, node.hex()[:8], message)


def _islinear(repo, revspec):
  revs = repo.revs(b'sort(%s, topo)' % (revspec,))
  lastctx = None
  for rev in reversed(list(revs)):
    ctx = repo[rev]
    if lastctx is not None and lastctx not in ctx.parents():
      return False
    lastctx = ctx
  return True


def _getone(repo, revspec, message=None):
  revs = repo.revs(revspec)
  if not revs:
    if message is not None:
      raise error.Abort(message)
    raise error.Abort(b'internal error: empty revset for "%s"' % (revspec,))
  if len(revs) > 1:
    if message is not None:
      raise error.Abort(message)
    raise error.Abort(b'internal error: multiple results in revset for "%s"' % (revspec,))
  return next(iter(revs))


def _evolve_opts():
  return {
      'dry_run': False,
      'confirm': False,
      'any': False,
      'rev': [],
      'bumped': False,
      'phase_divergent': False,
      'divergent': False,
      'content_divergent': False,
      'unstable': False,
      'orphan': False,
      'all': None,
      'update': False,
      'continue': False,
      'stop': False,
      'abort': False,
      'list': False,
      'tool': False,
  }


def _supersededsuccessorrev(repo, rev):
  node = repo.unfiltered()[rev].node()
  s = obsutil.successorssets(repo, node)
  assert len(s) == 1, 'unexpectedly divergent rev'
  assert len(s[0]) == 1, 'unexpectedly split rev'
  newnode = s[0][0]
  return repo[newnode].rev()


@command(
    b'qu-fold',
    [
        # pyformat: disable
        (b'', b'root', b'', b'google3 subdirectory containing patches/', b'DIR'),
        (b'', b'to', b'', b'quahog changeset to fold into (default: first ancestor '
                          b'rev with description starting with "#QUAHOG")', b'REV'),
        (b'', b'count', 0, b'number of patches to fold', b'NUM'),
        (b'', b'all', False, b'fold all patches'),
        (b'', b'rev', b'', b'patch revision(s) to fold', b'REVS'),
        # pyformat: enable
    ],
    b'hg qu-fold --root DIR [--count NUM | --rev REVS | --all] [--to REV]',
    helpcategory=command.CATEGORY_CHANGE_ORGANIZATION,
    helpbasic=True,
)
def fold(ui, repo, **opts):  # pylint: disable=g-doc-args
  """fold a patch change into a quahog change.

  Quahog operates on ``--root``'s patch set and supports popping (removing and
  applying a patch in a new commit) and folding (creating a .diff file and
  incorporating the commit into a quahog change).

  Quahog uses two types of special changesets:

    1. 'Quahog changesets' are the base changes that patches are popped from and
       folded into. They are identified by the prefix '#QUAHOG' in their change
       descriptions.
    2. 'Patch changesets' represent single patch files. They are identified by
       the prefix '[PATCH]' in their change descriptions.

  Fold operations require a Quahog change and one or more patch changes.

  ``--to`` can be used to specify the Quahog change using a revision. If not
  provided, it will be inferred from the current revision and from the patch
  changes specified. If no candidate is available, one will be created.

  The set of patches to fold can be specified in a number of ways:

    * ``--count`` infers the patch changes from the current revision or from the
      specified quahog change. It will fold in the first N successor patches to
      the quahog change.
    * ``--all`` uses a similar strategy as ``--count`` but consumes as many
      patch changes as possible.
    * ``--rev`` allows you to specify the exact patch changes to fold. They must
      form an unbroken chain from the quahog change.

  If no fold methods are specified, ``--count=1`` will be used.

  NOTE: To reorder patch changes, use ``histedit`` before folding.
  """
  opts = pycompat.byteskwargs(opts)
  rootpath = _trackedroot(ui, repo, opts.get(b'root'))
  createnew = False
  torevset = opts.get(b'to')
  foldrevset = opts.get(b'rev')
  foldall = opts.get(b'all')
  foldcount = opts.get(b'count')
  if foldcount < 0:
    ui.warn(b'--count must be positive\n')
    return
  if not any((foldrevset, foldall, foldcount)):
    foldcount = 1
  if foldrevset and foldall:
    raise error.Abort(b'cannot provide both --rev and --all')
  if foldrevset and foldcount:
    raise error.Abort(b'cannot provide both --rev and --count')
  with repo.wlock(), repo.lock(), repo.transaction(b'qu-fold') as txn:
    cmdutil.bailifchanged(repo)
    cmdutil.checkunfinished(repo, commit=True)
    seriespath = repo.wvfs.reljoin(rootpath, b'patches', b'series')
    if not repo.wvfs.isfile(seriespath):
      raise error.Abort(b'%s: no such file' % (seriespath,))
    if foldrevset and not _islinear(repo, foldrevset):
      raise error.Abort(b'--rev patches must be linear')
    origctx = repo[b'.']
    if torevset:
      torev = _getone(
          repo, torevset, message=b'--to must specify exactly one rev')
      toctx = repo[torev]
      if not _isquahogchange(toctx):
        raise error.Abort(_reverr(toctx, b'not a quahog changeset'))
      if toctx.phase() == phases.public:
        raise error.Abort(_reverr(toctx, b'immutable quahog changeset'))
      if foldrevset:
        ancestorrev = _getone(repo, b'ancestor((%s)^)' % (foldrevset,))
        if ancestorrev != torev:
          raise error.Abort(_reverr(toctx, b'--to must be parent of --rev'))
    elif foldrevset:
      torev = _getone(repo, b'ancestor((%s)^)' % (foldrevset,))
      toctx = repo[torev]
      if _isquahogpatch(toctx):
        raise error.Abort(_reverr(toctx, b'--rev parent cannot be a patch'))
      if not _isquahogchange(toctx):
        ui.warn(b'no quahog changeset found in ancestors; creating one\n')
        createnew = True
      elif toctx.phase() == phases.public:
        ui.warn(b'immutable quahog changeset found; creating new one\n')
        createnew = True
    else:  # default: find quahog changeset in ancestors of current rev
      toctx = repo[b'.']
      while _isquahogpatch(toctx) and not (toctx.phase() == phases.public or
                                           _isquahogchange(toctx)):
        if len(toctx.parents()) > 1:
          raise error.Abort(_reverr(toctx, b'multiple parents'))
        toctx = toctx.p1()
      if not _isquahogchange(toctx):
        ui.warn(b'no quahog changeset found in ancestors; creating one\n')
        createnew = True
      elif toctx.phase() == phases.public:
        ui.warn(b'immutable quahog changeset found; creating new one\n')
        createnew = True
    if foldrevset:
      patchrevs = repo.revs(foldrevset)
      for patchrev in patchrevs:
        patchctx = repo[patchrev]
        if not _isquahogpatch(patchctx):
          raise error.Abort(_reverr(patchctx, b'must be quahog patch to fold'))
        if patchctx.phase() == phases.public:
          raise error.Abort(_reverr(patchctx, b'cannot fold immutable patch'))
    else:
      patchrevs = []
      patchctx = toctx
      while foldall or len(patchrevs) != foldcount:
        patches = [ctx for ctx in patchctx.children() if _isquahogpatch(ctx)]
        if len(patches) > 1:
          raise error.Abort(
              _reverr(patchctx, b'ambiguous chain: multiple patch children')
          )
        if not patches:
          break
        patchctx = patches[0]
        patchrevs.append(patchctx.rev())
    if not patchrevs:
      ui.warn(b'no patches to fold\n')
      return
    ui.status(b'folding %d patch%s into "%s"\n' % (len(patchrevs), b'es' if len(patchrevs) > 1 else b'', rootpath))
    patches = {}
    for rev in patchrevs:
      # get patch name
      m = PATCHDESC_RE.fullmatch(repo[rev].description().splitlines()[0])
      assert m
      patchname = m.group(2).strip()
      origpatchname = patchname
      if b' ' in patchname:
        patchname = re.sub(rb' +', b'-', patchname)
      if patchname != origpatchname:
        ui.warn(b'rewriting patch "%s" to "%s"\n' % (origpatchname, patchname))
      # get patch description
      patchdesc = b'\n'.join(repo[rev].description().splitlines()[1:]).strip()
      patchcontent = b''
      if patchdesc:
        patchcontent += patchdesc + b'\n\n'
      # calculate diff
      diffopts = patch.diffallopts(ui, opts={b'noprefix': True, b'git': True})
      ctx2 = scmutil.revsingle(repo, rev, None)
      ctx1 = ctx2.p1()
      m = scmutil.match(ctx2, [b'path:' + rootpath], {})
      m = repo.narrowmatch(m)
      patchdiff = _hgdifftopatch(
          b''.join(
              patch.diff(repo, node1=ctx1, node2=ctx2, match=m, opts=diffopts)),
          rootpath,
      )
      # register patch file
      if patchdesc:
        patches[patchname] = patchdesc + b'\n\n' + patchdiff
      else:
        patches[patchname] = patchdiff
    # calculate the revs to rebase before modifying
    childrevs = repo.revs(b'children(%s) & not public()' %
                          (b'+'.join(repo[r].hex() for r in patchrevs),))
    childrevs -= patchrevs
    if createnew:
      # create new base rev and rebase all target changes
      ui.status(b'creating quahog changeset\n')
      merge.update(toctx, updatecheck=merge.UPDATECHECK_NO_CONFLICT)
      commitopts = opts.copy()
      g3relpath = rootpath[rootpath.index(b'/') + 1:]
      commitopts.update(
          {b'message': b'#QUAHOG Modify patches for %s.' % (g3relpath,)})
      with repo.ui.configoverride({(b'ui', b'allowemptycommit'): True},
                                  b'qu-fold'):
        newbase = cmdutil.commit(ui, repo, _basiccommitfunc, [], commitopts)
        assert newbase
        toctx = repo[newbase]
        ui.status(b'rebasing patches to quahog changeset\n')
        rebase.rebase(
            ui,
            repo,
            source=[repo[r].hex() for r in patchrevs],
            dest=toctx.hex(),
        )
      patchrevs = [_supersededsuccessorrev(repo, r) for r in patchrevs]
      childrevs = [_supersededsuccessorrev(repo, r) for r in childrevs]
    newrev = _fold_changesets(txn, repo, [toctx.rev()] + list(patchrevs))
    newctx = repo[newrev]
    merge.update(newctx, updatecheck=merge.UPDATECHECK_NO_CONFLICT)
    # add patch files
    for fname, content in patches.items():
      patchpath = repo.wvfs.reljoin(rootpath, b'patches', fname)
      ui.status(b'folding patch "%s"\n' % (fname,))
      with repo.wvfs(patchpath, b'wb') as patchfile:
        patchfile.write(content)
    repo[None].add([
        repo.wvfs.reljoin(rootpath, b'patches', fname)
        for fname, _ in patches.items()
    ])
    # add to series file
    with repo.wvfs(seriespath, b'ab') as seriesfile:
      seriesfile.write(b'\n'.join(patches.keys()) + b'\n')
    # amend changes
    rewriteutil.precheck(repo, [newctx.rev()], b'amend')
    amendopts = opts.copy()
    amendopts.update({b'edit': False})
    # NOTE: Not sure why this is required but a byteskwargs call ends up
    # failing as a result of the amend and this coercion fixes it.
    amendopts = pycompat.strkwargs(amendopts)
    newctx = cmdutil.amend(ui, repo, newctx, {}, [], amendopts)
    # rebase patch children onto new parent
    if childrevs:
      rebase.rebase(
          ui,
          repo,
          source=[repo[child].hex() for child in childrevs],
          dest=newctx.hex().encode('utf8'),
      )
    if origctx.rev() not in repo:
      successors = obsutil.successorssets(repo, origctx.node())
      origctx = repo[successors[0][0]]
    merge.update(origctx, updatecheck=merge.UPDATECHECK_NO_CONFLICT)


@command(
    b'qu-pop',
    [
        (b'', b'root', b'', b'google3 subdirectory containing patches/', b'DIR'),
        (b'', b'from', b'', b'quahog changeset to fold into (default: first ancestor '
                            b'rev with description starting with "#QUAHOG")', b'REV'),
        (b'', b'count', 1, b'number of patches to pop', b'NUM'),
        (b'', b'all', False, b'pop all patches'),
        (b'', b'rebase', b'', b'revisions to rebase to the patch commit. '
                              b'defaults to all children of ``from``.', b'REVS'),
    ],
    b'hg qu-pop --root DIR [--count NUM | --all] [--from REV] [--rebase REVS]',
    helpcategory=command.CATEGORY_CHANGE_ORGANIZATION,
    helpbasic=True,
)
def pop(ui, repo, **opts):  # pylint: disable=g-doc-args
  """pop a patch into a quahog change.

  Quahog operates on ``--root``'s patch set and supports popping (removing and
  applying a patch in a new commit) and folding (creating a .diff file and
  incorporating the commit into a quahog change).

  Quahog uses two types of special changesets:

    1. 'Quahog changesets' are the base changes that patches are popped from and
       folded into. They are identified by the prefix '#QUAHOG' in their change
       descriptions.
    2. 'Patch changesets' represent single patch files. They are identified by
       the prefix '[PATCH]' in their change descriptions.

  Pop operations create one or patch changes on a Quahog change.

  ``--from`` can be used to specify the Quahog change using a revision. If not
  provided, it will be inferred from the current revision. If no candidate is
  available, one will be created.

  The set of patches to pop can be specified in a number of ways:

    * ``--count`` specifies the number of patch changes to create from the end
      of the patches/series file.
    * ``--all`` uses a similar strategy as ``--count`` but consumes all patch
      files specified in the patches/series.
  """
  opts = pycompat.byteskwargs(opts)
  rootpath = _trackedroot(ui, repo, opts.get(b'root'))
  createnew = False
  patchestopop = opts.get(b'count')
  if patchestopop < 1:
    ui.warn(b'no pops requested\n')
    return
  popall = opts.get(b'all')
  with repo.wlock(), repo.lock(), repo.transaction(b'qu-pop'):
    cmdutil.bailifchanged(repo)
    cmdutil.checkunfinished(repo, commit=True)
    origctx = repo[b'.']
    if opts.get(b'from'):
      fromrev = _getone(
          repo,
          opts.get(b'from'),
          message=b'--from must specify exactly one rev')
      fromctx = repo[fromrev]
      if not _isquahogchange(fromctx):
        raise error.Abort(_reverr(fromctx, b'not a quahog changset'))
    else:
      fromctx = repo[b'.']
      while fromctx.phase() != phases.public and not _isquahogchange(fromctx):
        fromctx = fromctx.p1()
      if not _isquahogchange(fromctx):
        ui.warn(b'no quahog changeset found in ancestors; creating one\n')
        fromctx = repo[b'.']
        createnew = True
    merge.update(fromctx, updatecheck=merge.UPDATECHECK_NO_CONFLICT)
    rebaserevs = repo.revs(
        opts.get(b'rebase')
        # Default to all children of the `from` quahog changeset, if it exists.
        or (b'none()' if createnew else b'children(. & not public())')
    )
    seriespath = repo.wvfs.reljoin(rootpath, b'patches', b'series')
    if not repo.wvfs.isfile(seriespath):
      raise error.Abort(b'%s: no such file' % (seriespath,))
    with repo.wvfs(seriespath) as seriesfile:
      origseries = seriesfile.read()
    patches = origseries.splitlines()
    patches = [p for p in patches if p and not p.startswith(b'#')]
    if popall:
      patchestopop = len(patches)
      if patchestopop < 1:
        ui.warn(b'no patches to pop\n')
        return
    ui.status(b'popping %d patch%s from "%s"\n' % (patchestopop, b'es' if patchestopop > 1 else b'', rootpath))
    try:
      patchinfos = []
      for i, patchtopop in zip(range(patchestopop), reversed(patches)):
        with repo.wvfs(seriespath, b'wb') as seriesfile:
          seriesfile.write(b'\n'.join(patches[:-i-1]) + b'\n')
        patchpath = repo.wvfs.reljoin(rootpath, b'patches', patchtopop)
        with repo.wvfs(patchpath) as patchfile:
          patchcontent = patchfile.read()
        patchcontent, patchdesc = _separatepatchdescription(patchcontent)
        patchcontent = _canonicalizepatchcontent(patchcontent, rootpath)
        repo[None].forget([patchpath])
        repo.wvfs.unlinkpath(patchpath)
        # TODO: Convert to use mercurial's patch API.
        try:
          subprocess.run(
              [b'git', b'apply', b'--reverse', b'--directory', rootpath, b'-'],
              input=patchcontent,
              check=True,
              cwd=repo.root,
              capture_output=True)
        except subprocess.CalledProcessError as e:
          ui.warn(b'command failed for %r: %r\n' % (patchpath, e.cmd))
          ui.warn(e.stderr)
          raise error.Abort(b'failed to reverse patch')
        patchinfos.append((patchtopop, patchcontent, patchdesc))
      repo.invalidatecaches()
      status = repo.status(unknown=True)
      repo[None].forget(status.deleted)
      repo[None].add(status.unknown)
      if createnew:
        commitopts = opts.copy()
        g3relpath = rootpath[rootpath.index(b'/')+1:]
        commitopts.update(
            {b'message': b'#QUAHOG Modify patches for %s.' % (g3relpath,)})
        assert cmdutil.commit(ui, repo, _basiccommitfunc, [], commitopts)
      else:
        rewriteutil.precheck(repo, [fromctx.rev()], b'amend')
        amendopts = opts.copy()
        amendopts.update({b'edit': False})
        # NOTE: Not sure why this is required but a byteskwargs call ends up
        # failing as a result of the amend and this coercion fixes it.
        amendopts = pycompat.strkwargs(amendopts)
        assert cmdutil.amend(ui, repo, fromctx, {}, [], amendopts)
      for patchtopop, patchcontent, patchdesc in reversed(patchinfos):
        ui.status(b'popping patch "%s"\n' % (patchtopop,))
        # TODO: Convert to use mercurial's patch API.
        try:
          subprocess.run([b'git', b'apply', b'--directory', rootpath, b'-'],
                         input=patchcontent,
                         check=True,
                         cwd=repo.root,
                         capture_output=True)
        except subprocess.CalledProcessError as e:
          ui.warn(b'command failed: %r\n' % (e.cmd,))
          ui.warn(e.stderr)
          raise error.Abort(b'%s: failed to apply patch' % (patchtopop,))
        else:
          repo.invalidatecaches()
          status = repo.status(unknown=True)
          repo[None].forget(status.deleted)
          repo[None].add(status.unknown)
        patchmessage = b'DO %s SUBMIT [PATCH] %s' % (b'NOT', patchtopop)
        if patchdesc:
          patchmessage += b'\n\n' + patchdesc
        commitopts = opts.copy()
        commitopts.update({b'message': patchmessage})
        leaf = cmdutil.commit(ui, repo, _basiccommitfunc, [], commitopts)
        assert leaf
      if rebaserevs:
        rebase.rebase(
            ui,
            repo,
            source=[repo[child].hex() for child in rebaserevs],
            dest=repo[leaf].hex())
      if origctx.rev() not in repo:
        successors = obsutil.successorssets(repo, origctx.node())
        origctx = repo[successors[0][0]]
      merge.update(origctx, updatecheck=merge.UPDATECHECK_NO_CONFLICT)
    except:  # pylint: disable=bare-except
      with repo.wvfs(seriespath, b'wb') as seriesfile:
        seriesfile.write(origseries)
      raise
