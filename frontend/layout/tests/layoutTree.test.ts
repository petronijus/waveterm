// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { assert, test } from "vitest";
import { newLayoutNode } from "../lib/layoutNode";
import { computeMoveNode, moveNode, setTree } from "../lib/layoutTree";
import {
    DropDirection,
    FlexDirection,
    LayoutTreeActionType,
    LayoutTreeComputeMoveNodeAction,
    LayoutTreeMoveNodeAction,
} from "../lib/types";
import { newLayoutTreeState } from "./model";

test("layoutTreeStateReducer - compute move", () => {
    const nodeA = newLayoutNode(undefined, undefined, undefined, { blockId: "nodeA" });
    const node1 = newLayoutNode(undefined, undefined, undefined, { blockId: "node1" });
    const node2 = newLayoutNode(undefined, undefined, undefined, { blockId: "node2" });
    const treeState = newLayoutTreeState(newLayoutNode(undefined, undefined, [nodeA, node1, node2]));
    assert(treeState.rootNode.children!.length === 3, "root should have three children");
    let pendingAction = computeMoveNode(treeState, {
        type: LayoutTreeActionType.ComputeMove,
        nodeId: treeState.rootNode.id,
        nodeToMoveId: node1.id,
        direction: DropDirection.Bottom,
    });
    const insertOperation = pendingAction as LayoutTreeMoveNodeAction;
    assert(insertOperation.node === node1, "insert operation node should equal node1");
    assert(!insertOperation.parentId, "insert operation parent should not be defined");
    assert(insertOperation.index === 1, "insert operation index should equal 1");
    assert(insertOperation.insertAtRoot, "insert operation insertAtRoot should be true");
    moveNode(treeState, insertOperation);
    assert(
        treeState.rootNode.data === undefined && treeState.rootNode.children!.length === 3,
        "root node should still have three children"
    );
    assert(treeState.rootNode.children![1].data!.blockId === "node1", "root's second child should be node1");

    pendingAction = computeMoveNode(treeState, {
        type: LayoutTreeActionType.ComputeMove,
        nodeId: node1.id,
        nodeToMoveId: node2.id,
        direction: DropDirection.Bottom,
    });
    const insertOperation2 = pendingAction as LayoutTreeMoveNodeAction;
    assert(insertOperation2.node === node2, "insert operation node should equal node2");
    assert(insertOperation2.parentId === node1.id, "insert operation parent id should be node1 id");
    assert(insertOperation2.index === 1, "insert operation index should equal 1");
    assert(!insertOperation2.insertAtRoot, "insert operation insertAtRoot should be false");
    moveNode(treeState, insertOperation2);
    assert(
        treeState.rootNode.data === undefined && (treeState.rootNode.children!.length as number) === 2,
        "root node should now have two children after node2 moved into node1"
    );
    assert(treeState.rootNode.children![1].children!.length === 2, "root's second child should now have two children");
});

test("computeMove - noop action", () => {
    const nodeToMove = newLayoutNode(undefined, undefined, undefined, { blockId: "nodeToMove" });
    const treeState = newLayoutTreeState(
        newLayoutNode(undefined, undefined, [
            nodeToMove,
            newLayoutNode(undefined, undefined, undefined, { blockId: "otherNode" }),
        ])
    );
    let moveAction: LayoutTreeComputeMoveNodeAction = {
        type: LayoutTreeActionType.ComputeMove,
        nodeId: treeState.rootNode.id,
        nodeToMoveId: nodeToMove.id,
        direction: DropDirection.Left,
    };
    let pendingAction = computeMoveNode(treeState, moveAction);

    assert(pendingAction === undefined, "inserting a node to the left of itself should not produce a pendingAction");

    moveAction = {
        type: LayoutTreeActionType.ComputeMove,
        nodeId: treeState.rootNode.id,
        nodeToMoveId: nodeToMove.id,
        direction: DropDirection.Right,
    };

    pendingAction = computeMoveNode(treeState, moveAction);
    assert(pendingAction === undefined, "inserting a node to the right of itself should not produce a pendingAction");
});

test("setTree - replaces tree wholesale and carries focus/magnify", () => {
    const oldNode = newLayoutNode(undefined, undefined, undefined, { blockId: "oldNode" });
    const treeState = newLayoutTreeState(
        newLayoutNode(undefined, undefined, [
            oldNode,
            newLayoutNode(undefined, undefined, undefined, { blockId: "otherOldNode" }),
        ])
    );
    treeState.focusedNodeId = oldNode.id;

    const newLeafA = newLayoutNode(FlexDirection.Column, 12.5, undefined, { blockId: "newA" });
    const newLeafB = newLayoutNode(FlexDirection.Column, 7.5, undefined, { blockId: "newB" });
    const newRoot = newLayoutNode(FlexDirection.Row, 20, [newLeafA, newLeafB]);
    setTree(treeState, {
        type: LayoutTreeActionType.SetTree,
        rootNode: newRoot,
        focusedNodeId: newLeafB.id,
        magnifiedNodeId: undefined,
    });

    assert(treeState.rootNode === newRoot, "root node should be the new tree");
    assert(treeState.rootNode.children!.length === 2, "new root should have two children");
    assert(treeState.rootNode.children![0].size === 12.5, "sizes should carry over exactly");
    assert(treeState.focusedNodeId === newLeafB.id, "focus should follow the new tree");
    assert(treeState.magnifiedNodeId === undefined, "magnify should be cleared when not provided");
    assert(treeState.leafOrder === undefined, "leafOrder must reset so updateTree recomputes it");
});

test("setTree - missing rootNode is a no-op", () => {
    const existingRoot = newLayoutNode(undefined, undefined, undefined, { blockId: "existing" });
    const treeState = newLayoutTreeState(existingRoot);
    setTree(treeState, {
        type: LayoutTreeActionType.SetTree,
        rootNode: undefined,
    });
    assert(treeState.rootNode === existingRoot, "tree should be untouched when rootNode is missing");
});
